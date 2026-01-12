// Copyright 2017 fatedier, fatedier@gmail.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/lo"

	"github.com/fatedier/frp/client/proxy"
	"github.com/fatedier/frp/client/visitor"
	"github.com/fatedier/frp/pkg/auth"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/transport"
	netpkg "github.com/fatedier/frp/pkg/util/net"
	"github.com/fatedier/frp/pkg/util/wait"
	"github.com/fatedier/frp/pkg/util/xlog"
	"github.com/fatedier/frp/pkg/vnet"
)

type SessionContext struct {
	// The client common configuration.
	Common *v1.ClientCommonConfig

	// Unique ID obtained from frps.
	// It should be attached to the login message when reconnecting.
	RunID string
	// Underlying control connection. Once conn is closed, the msgDispatcher and the entire Control will exit.
	Conn net.Conn
	// Indicates whether the connection is encrypted.
	ConnEncrypted bool
	// Auth runtime used for login, heartbeats, and encryption.
	Auth *auth.ClientAuth
	// Connector is used to create new connections, which could be real TCP connections or virtual streams.
	Connector Connector
	// Virtual net controller
	VnetController *vnet.Controller
}

type Control struct {
	// service context
	ctx context.Context
	xl  *xlog.Logger

	// session context
	sessionCtx *SessionContext

	// manage all proxies
	pm *proxy.Manager

	// manage all visitors
	vm *visitor.Manager

	doneCh chan struct{}

	// of time.Time, last time got the Pong message
	lastPong atomic.Value

	// The role of msgTransporter is similar to HTTP2.
	// It allows multiple messages to be sent simultaneously on the same control connection.
	// The server's response messages will be dispatched to the corresponding waiting goroutines based on the laneKey and message type.
	msgTransporter transport.MessageTransporter

	// msgDispatcher is a wrapper for control connection.
	// It provides a channel for sending messages, and you can register handlers to process messages based on their respective types.
	msgDispatcher *msg.Dispatcher
}

func NewControl(ctx context.Context, sessionCtx *SessionContext) (*Control, error) {
	// new xlog instance
	ctl := &Control{
		ctx:        ctx,
		xl:         xlog.FromContextSafe(ctx),
		sessionCtx: sessionCtx,
		doneCh:     make(chan struct{}),
	}
	ctl.lastPong.Store(time.Now())

	if sessionCtx.ConnEncrypted {
		cryptoRW, err := netpkg.NewCryptoReadWriter(sessionCtx.Conn, sessionCtx.Auth.EncryptionKey())
		if err != nil {
			return nil, err
		}
		ctl.msgDispatcher = msg.NewDispatcher(cryptoRW)
	} else {
		ctl.msgDispatcher = msg.NewDispatcher(sessionCtx.Conn)
	}
	ctl.registerMsgHandlers()
	ctl.msgTransporter = transport.NewMessageTransporter(ctl.msgDispatcher)

	ctl.pm = proxy.NewManager(ctl.ctx, sessionCtx.Common, sessionCtx.Auth.EncryptionKey(), ctl.msgTransporter, sessionCtx.VnetController)
	ctl.vm = visitor.NewManager(ctl.ctx, sessionCtx.RunID, sessionCtx.Common,
		ctl.connectServer, ctl.msgTransporter, sessionCtx.VnetController)
	return ctl, nil
}

func (ctl *Control) Run(proxyCfgs []v1.ProxyConfigurer, visitorCfgs []v1.VisitorConfigurer) {
	go ctl.worker()

	// start all proxies
	ctl.pm.UpdateAll(proxyCfgs)

	// start all visitors
	ctl.vm.UpdateAll(visitorCfgs)
}

func (ctl *Control) SetInWorkConnCallback(cb func(*v1.ProxyBaseConfig, net.Conn, *msg.StartWorkConn) bool) {
	ctl.pm.SetInWorkConnCallback(cb)
}

func (ctl *Control) handleReqWorkConn(_ msg.Message) {
	xl := ctl.xl
	start := time.Now()
	workConn, err := ctl.connectServer()
	if err != nil {
		xl.Warnf("start new connection to server error: %v", err)
		return
	}

	var (
		msc MuxSessionConnector
		idx = -1
	)
	if v, ok := ctl.sessionCtx.Connector.(MuxSessionConnector); ok {
		msc = v
		if w, ok := workConn.(interface{ MuxSessionIndex() int }); ok {
			idx = w.MuxSessionIndex()
		}
	}

	m := &msg.NewWorkConn{
		RunID: ctl.sessionCtx.RunID,
	}
	if err = ctl.sessionCtx.Auth.Setter.SetNewWorkConn(m); err != nil {
		xl.Warnf("error during NewWorkConn authentication: %v", err)
		if msc != nil && idx >= 0 {
			msc.ReportMuxLinkProbeResult(idx, 0, err)
		}
		workConn.Close()
		return
	}
	if err = msg.WriteMsg(workConn, m); err != nil {
		xl.Warnf("work connection write to server error: %v", err)
		if msc != nil && idx >= 0 {
			msc.ReportMuxLinkProbeResult(idx, 0, err)
		}
		workConn.Close()
		return
	}

	var startMsg msg.StartWorkConn
	if err = msg.ReadMsgInto(workConn, &startMsg); err != nil {
		xl.Tracef("work connection closed before response StartWorkConn message: %v", err)
		if msc != nil && idx >= 0 {
			msc.ReportMuxLinkProbeResult(idx, 0, err)
		}
		workConn.Close()
		return
	}
	if startMsg.Error != "" {
		xl.Errorf("StartWorkConn contains error: %s", startMsg.Error)
		if msc != nil && idx >= 0 {
			msc.ReportMuxLinkProbeResult(idx, 0, fmt.Errorf("%s", startMsg.Error))
		}
		workConn.Close()
		return
	}

	if msc != nil && idx >= 0 {
		msc.ReportMuxLinkProbeResult(idx, time.Since(start), nil)
	}

	// dispatch this work connection to related proxy
	ctl.pm.HandleWorkConn(startMsg.ProxyName, workConn, &startMsg)
}

func (ctl *Control) handleNewProxyResp(m msg.Message) {
	xl := ctl.xl
	inMsg := m.(*msg.NewProxyResp)
	// Server will return NewProxyResp message to each NewProxy message.
	// Start a new proxy handler if no error got
	err := ctl.pm.StartProxy(inMsg.ProxyName, inMsg.RemoteAddr, inMsg.Error)
	if err != nil {
		xl.Warnf("[%s] start error: %v", inMsg.ProxyName, err)
	} else {
		xl.Infof("[%s] start proxy success", inMsg.ProxyName)
	}
}

func (ctl *Control) handleNatHoleResp(m msg.Message) {
	xl := ctl.xl
	inMsg := m.(*msg.NatHoleResp)

	// Dispatch the NatHoleResp message to the related proxy.
	ok := ctl.msgTransporter.DispatchWithType(inMsg, msg.TypeNameNatHoleResp, inMsg.TransactionID)
	if !ok {
		xl.Tracef("dispatch NatHoleResp message to related proxy error")
	}
}

func (ctl *Control) handlePong(m msg.Message) {
	xl := ctl.xl
	inMsg := m.(*msg.Pong)

	if inMsg.Error != "" {
		xl.Errorf("pong message contains error: %s", inMsg.Error)
		ctl.closeSession()
		return
	}
	ctl.lastPong.Store(time.Now())
	xl.Debugf("receive heartbeat from server")
}

// closeSession closes the control connection.
func (ctl *Control) closeSession() {
	ctl.sessionCtx.Conn.Close()
	ctl.sessionCtx.Connector.Close()
}

func (ctl *Control) Close() error {
	return ctl.GracefulClose(0)
}

func (ctl *Control) GracefulClose(d time.Duration) error {
	ctl.pm.Close()
	ctl.vm.Close()

	time.Sleep(d)

	ctl.closeSession()
	return nil
}

// Done returns a channel that will be closed after all resources are released
func (ctl *Control) Done() <-chan struct{} {
	return ctl.doneCh
}

// connectServer return a new connection to frps
func (ctl *Control) connectServer() (net.Conn, error) {
	return ctl.sessionCtx.Connector.Connect()
}

func (ctl *Control) registerMsgHandlers() {
	ctl.msgDispatcher.RegisterHandler(&msg.ReqWorkConn{}, msg.AsyncHandler(ctl.handleReqWorkConn))
	ctl.msgDispatcher.RegisterHandler(&msg.NewProxyResp{}, ctl.handleNewProxyResp)
	ctl.msgDispatcher.RegisterHandler(&msg.NatHoleResp{}, ctl.handleNatHoleResp)
	ctl.msgDispatcher.RegisterHandler(&msg.Pong{}, ctl.handlePong)
}

// heartbeatWorker sends heartbeat to server and check heartbeat timeout.
func (ctl *Control) heartbeatWorker() {
	xl := ctl.xl

	if ctl.sessionCtx.Common.Transport.HeartbeatInterval > 0 {
		// Send heartbeat to server.
		sendHeartBeat := func() (bool, error) {
			xl.Debugf("send heartbeat to server")
			pingMsg := &msg.Ping{}
			if err := ctl.sessionCtx.Auth.Setter.SetPing(pingMsg); err != nil {
				xl.Warnf("error during ping authentication: %v, skip sending ping message", err)
				return false, err
			}
			_ = ctl.msgDispatcher.Send(pingMsg)
			return false, nil
		}

		go wait.BackoffUntil(sendHeartBeat,
			wait.NewFastBackoffManager(wait.FastBackoffOptions{
				Duration:           time.Duration(ctl.sessionCtx.Common.Transport.HeartbeatInterval) * time.Second,
				InitDurationIfFail: time.Second,
				Factor:             2.0,
				Jitter:             0.1,
				MaxDuration:        time.Duration(ctl.sessionCtx.Common.Transport.HeartbeatInterval) * time.Second,
			}),
			true, ctl.doneCh,
		)
	}

	// Check heartbeat timeout.
	if ctl.sessionCtx.Common.Transport.HeartbeatInterval > 0 && ctl.sessionCtx.Common.Transport.HeartbeatTimeout > 0 {
		go wait.Until(func() {
			if time.Since(ctl.lastPong.Load().(time.Time)) > time.Duration(ctl.sessionCtx.Common.Transport.HeartbeatTimeout)*time.Second {
				xl.Warnf("heartbeat timeout")
				ctl.closeSession()
				return
			}
		}, time.Second, ctl.doneCh)
	}
}

func (ctl *Control) worker() {
	xl := ctl.xl
	go ctl.heartbeatWorker()
	go ctl.linkProbeWorker()
	go ctl.msgDispatcher.Run()

	<-ctl.msgDispatcher.Done()
	xl.Debugf("control message dispatcher exited")
	ctl.closeSession()

	ctl.pm.Close()
	ctl.vm.Close()
	close(ctl.doneCh)
}

func (ctl *Control) UpdateAllConfigurer(proxyCfgs []v1.ProxyConfigurer, visitorCfgs []v1.VisitorConfigurer) error {
	ctl.vm.UpdateAll(visitorCfgs)
	ctl.pm.UpdateAll(proxyCfgs)
	return nil
}

// linkProbeWorker probes each tcp mux session via Ping/Pong, so that the connector
// can choose a better underlying TCP session for new streams.
func (ctl *Control) linkProbeWorker() {
	msc, ok := ctl.sessionCtx.Connector.(MuxSessionConnector)
	if !ok {
		return
	}
	if !lo.FromPtr(ctl.sessionCtx.Common.Transport.TCPMux) {
		return
	}
	sessionCount := msc.MuxSessionCount()
	if sessionCount <= 1 {
		return
	}

	mode := strings.ToLower(strings.TrimSpace(ctl.sessionCtx.Common.Transport.TCPMuxLinkProbeMode))
	if mode == "" {
		mode = "auto"
	}
	if mode == "off" || mode == "disable" {
		mode = "disabled"
	}
	if mode == "disabled" || mode == "passive" {
		return
	}

	intervalSec := ctl.sessionCtx.Common.Transport.TCPMuxLinkProbeInterval
	timeoutSec := ctl.sessionCtx.Common.Transport.TCPMuxLinkProbeTimeout
	if timeoutSec <= 0 {
		timeoutSec = 3
	}

	timeout := time.Duration(timeoutSec) * time.Second

	var probeInFlight atomic.Bool
	var probeDisabled atomic.Bool
	var probeRound atomic.Int64

	// In auto/active mode, try a cheap startup probe first to avoid repeated probe traffic on older frps versions.
	// - If supported: in auto mode, enable periodic probing even when interval isn't set.
	// - If not supported: disable and log once.
	// - If unknown (e.g., transient network error): fall back to passive and rely on workConn handshakes.
	if mode == "auto" || mode == "active" {
		supported, decided := ctl.tryDetectProbeSupport(msc, timeout, sessionCount)
		if decided && !supported {
			probeDisabled.Store(true)
			ctl.xl.Infof("tcp mux link probe disabled: frps doesn't support probe on new streams")
			return
		}
		if mode == "auto" {
			if decided && supported {
				ctl.xl.Infof("tcp mux link probe enabled: frps supports probe on new streams")
				if intervalSec <= 0 {
					intervalSec = 10
				}
			} else if intervalSec <= 0 {
				ctl.xl.Infof("tcp mux link probe skipped: probe support unknown and interval not set (fall back to passive)")
				return
			}
		}
	}
	if intervalSec <= 0 {
		return
	}

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	probeOnce := func() {
		if probeDisabled.Load() {
			return
		}
		if probeInFlight.Swap(true) {
			return
		}
		defer probeInFlight.Store(false)

		round := probeRound.Add(1)
		var successCount atomic.Int64
		var eofQuickCount atomic.Int64

		var wg sync.WaitGroup
		wg.Add(sessionCount)
		for i := 0; i < sessionCount; i++ {
			// Run each probe in its own goroutine so a slow link doesn't block others.
			go func(idx int) {
				defer wg.Done()

				start := time.Now()
				conn, err := msc.ConnectBySessionIndex(idx)
				if err != nil {
					msc.ReportMuxLinkProbeResult(idx, 0, err)
					return
				}
				defer conn.Close()

				_ = conn.SetDeadline(time.Now().Add(timeout))

				ping := &msg.Ping{}
				if err := ctl.sessionCtx.Auth.Setter.SetPing(ping); err != nil {
					msc.ReportMuxLinkProbeResult(idx, 0, err)
					return
				}
				if err := msg.WriteMsg(conn, ping); err != nil {
					msc.ReportMuxLinkProbeResult(idx, 0, err)
					return
				}
				var pong msg.Pong
				if err := msg.ReadMsgInto(conn, &pong); err != nil {
					// Backward-compat: older frps versions don't handle Ping as the first message on a new stream,
					// so they will close the connection without responding.
					if errors.Is(err, io.EOF) && time.Since(start) < timeout {
						eofQuickCount.Add(1)
					}
					msc.ReportMuxLinkProbeResult(idx, 0, err)
					return
				}
				if pong.Error != "" {
					msc.ReportMuxLinkProbeResult(idx, 0, fmt.Errorf("%s", pong.Error))
					return
				}
				successCount.Add(1)
				msc.ReportMuxLinkProbeResult(idx, time.Since(start), nil)
			}(i)
		}
		wg.Wait()

		// If all probes failed with EOF quickly on the first round, assume frps doesn't support probe and disable it.
		if round == 1 && successCount.Load() == 0 && eofQuickCount.Load() == int64(sessionCount) {
			probeDisabled.Store(true)
			ctl.xl.Infof("tcp mux link probe disabled: frps doesn't support probe on new streams")
		}
	}

	// Probe immediately, then periodically.
	probeOnce()
	for {
		select {
		case <-ctl.doneCh:
			return
		case <-ticker.C:
			probeOnce()
		}
	}
}

// tryDetectProbeSupport attempts a few probe requests and returns (supported, decided).
// decided==true means we are confident about support (e.g., all attempts got EOF quickly -> unsupported, or got a valid Pong -> supported).
func (ctl *Control) tryDetectProbeSupport(msc MuxSessionConnector, timeout time.Duration, sessionCount int) (supported bool, decided bool) {
	// Keep this very lightweight: use a single session (index 0) to avoid noisy logs on older frps versions.
	maxTry := 1

	eofQuick := 0
	for i := 0; i < maxTry; i++ {
		idx := 0
		if idx >= sessionCount {
			return false, false
		}
		start := time.Now()
		conn, err := msc.ConnectBySessionIndex(idx)
		if err != nil {
			continue
		}
		func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(timeout))

			ping := &msg.Ping{}
			if err := ctl.sessionCtx.Auth.Setter.SetPing(ping); err != nil {
				return
			}
			if err := msg.WriteMsg(conn, ping); err != nil {
				return
			}
			var pong msg.Pong
			if err := msg.ReadMsgInto(conn, &pong); err != nil {
				if errors.Is(err, io.EOF) && time.Since(start) < timeout {
					eofQuick++
				}
				return
			}
			if pong.Error != "" {
				// If server returns an error, we treat it as "supported but rejected".
				supported = true
				decided = true
				return
			}
			supported = true
			decided = true
		}()
		if decided {
			return supported, decided
		}
	}
	if eofQuick == maxTry {
		return false, true
	}
	return false, false
}
