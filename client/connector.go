// Copyright 2023 The frp Authors
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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	libnet "github.com/fatedier/golib/net"
	fmux "github.com/hashicorp/yamux"
	quic "github.com/quic-go/quic-go"
	"github.com/samber/lo"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/transport"
	netpkg "github.com/fatedier/frp/pkg/util/net"
	"github.com/fatedier/frp/pkg/util/xlog"
)

type MuxSessionConnector interface {
	Connector

	// MuxSessionCount returns the number of active TCP mux sessions.
	// Returns 0 when TCP mux isn't enabled or isn't active.
	MuxSessionCount() int
	// ConnectBySessionIndex opens a stream on a specified TCP mux session.
	ConnectBySessionIndex(index int) (net.Conn, error)

	// ReportMuxLinkProbeResult reports an RTT sample for a session.
	ReportMuxLinkProbeResult(index int, rtt time.Duration, err error)
}

// Connector is an interface for establishing connections to the server.
type Connector interface {
	Open() error
	Connect() (net.Conn, error)
	Close() error
}

type muxStreamConn struct {
	net.Conn
	muxSessionIndex int
}

func (c *muxStreamConn) MuxSessionIndex() int { return c.muxSessionIndex }

type muxLinkStats struct {
	activeStreams atomic.Int64

	mu sync.Mutex
	// moving averages (nanoseconds)
	openStreamEWMA float64
	rttEWMA        float64
	errEWMA        float64

	consecutiveErrors int
	unhealthyUntil    time.Time
}

func (s *muxLinkStats) warm() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rttEWMA > 0 || s.openStreamEWMA > 0
}

func (s *muxLinkStats) healthy(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unhealthyUntil.IsZero() || now.After(s.unhealthyUntil)
}

func (s *muxLinkStats) score(now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.unhealthyUntil.IsZero() && now.Before(s.unhealthyUntil) {
		return 1e18
	}

	activePenalty := float64(s.activeStreams.Load()) * float64((500 * time.Microsecond).Nanoseconds())

	// Prefer RTT when we have it, otherwise fall back to openStream latency.
	base := s.rttEWMA
	if base <= 0 {
		base = s.openStreamEWMA
	}
	if base <= 0 {
		base = float64((2 * time.Millisecond).Nanoseconds())
	}

	errPenalty := s.errEWMA * float64((200 * time.Millisecond).Nanoseconds())
	return base + activePenalty + errPenalty
}

func ewmaUpdate(prev float64, sample float64, alpha float64) float64 {
	if prev <= 0 {
		return sample
	}
	return prev*(1-alpha) + sample*alpha
}

// defaultConnectorImpl is the default implementation of Connector for normal frpc.
type defaultConnectorImpl struct {
	ctx context.Context
	cfg *v1.ClientCommonConfig

	mu sync.RWMutex

	muxSessions []*fmux.Session
	muxStats    []*muxLinkStats
	quicConn    *quic.Conn

	firstConnectUsed atomic.Bool
	rrCounter        atomic.Uint32
	closeOnce        sync.Once
	closed           atomic.Bool
}

func NewConnector(ctx context.Context, cfg *v1.ClientCommonConfig) Connector {
	return &defaultConnectorImpl{
		ctx: ctx,
		cfg: cfg,
	}
}

// Open opens an underlying connection to the server.
// The underlying connection is either a TCP connection or a QUIC connection.
// After the underlying connection is established, you can call Connect() to get a stream.
// If TCPMux isn't enabled, the underlying connection is nil, you will get a new real TCP connection every time you call Connect().
func (c *defaultConnectorImpl) Open() error {
	xl := xlog.FromContextSafe(c.ctx)

	// special for quic
	if strings.EqualFold(c.cfg.Transport.Protocol, "quic") {
		var tlsConfig *tls.Config
		var err error
		sn := c.cfg.Transport.TLS.ServerName
		if sn == "" {
			sn = c.cfg.ServerAddr
		}
		if lo.FromPtr(c.cfg.Transport.TLS.Enable) {
			tlsConfig, err = transport.NewClientTLSConfig(
				c.cfg.Transport.TLS.CertFile,
				c.cfg.Transport.TLS.KeyFile,
				c.cfg.Transport.TLS.TrustedCaFile,
				sn)
		} else {
			tlsConfig, err = transport.NewClientTLSConfig("", "", "", sn)
		}
		if err != nil {
			xl.Warnf("fail to build tls configuration, err: %v", err)
			return err
		}
		tlsConfig.NextProtos = []string{"frp"}

		conn, err := quic.DialAddr(
			c.ctx,
			net.JoinHostPort(c.cfg.ServerAddr, strconv.Itoa(c.cfg.ServerPort)),
			tlsConfig, &quic.Config{
				MaxIdleTimeout:     time.Duration(c.cfg.Transport.QUIC.MaxIdleTimeout) * time.Second,
				MaxIncomingStreams: int64(c.cfg.Transport.QUIC.MaxIncomingStreams),
				KeepAlivePeriod:    time.Duration(c.cfg.Transport.QUIC.KeepalivePeriod) * time.Second,
			})
		if err != nil {
			return err
		}
		c.quicConn = conn
		return nil
	}

	if !lo.FromPtr(c.cfg.Transport.TCPMux) {
		return nil
	}

	sessionCount := c.cfg.Transport.TCPMuxSessionCount
	if sessionCount < 1 {
		sessionCount = 1
	}

	fmuxCfg := fmux.DefaultConfig()
	fmuxCfg.KeepAliveInterval = time.Duration(c.cfg.Transport.TCPMuxKeepaliveInterval) * time.Second
	// Use trace level for yamux logs
	fmuxCfg.LogOutput = xlog.NewTraceWriter(xl)
	fmuxCfg.MaxStreamWindowSize = 6 * 1024 * 1024

	var lastErr error
	sessions := make([]*fmux.Session, 0, sessionCount)
	stats := make([]*muxLinkStats, 0, sessionCount)
	for i := 0; i < sessionCount; i++ {
		conn, err := c.realConnect()
		if err != nil {
			lastErr = err
			xl.Warnf("tcp mux session dial error (index=%d): %v", i, err)
			continue
		}
		session, err := fmux.Client(conn, fmuxCfg)
		if err != nil {
			lastErr = err
			xl.Warnf("create tcp mux session error (index=%d): %v", i, err)
			_ = conn.Close()
			continue
		}
		sessions = append(sessions, session)
		stats = append(stats, &muxLinkStats{})
	}
	if len(sessions) == 0 {
		if lastErr == nil {
			lastErr = errors.New("no tcp mux session available")
		}
		return lastErr
	}

	c.mu.Lock()
	c.muxSessions = sessions
	c.muxStats = stats
	c.mu.Unlock()
	return nil
}

// Connect returns a stream from the underlying connection, or a new TCP connection if TCPMux isn't enabled.
func (c *defaultConnectorImpl) Connect() (net.Conn, error) {
	if c.quicConn != nil {
		stream, err := c.quicConn.OpenStreamSync(context.Background())
		if err != nil {
			return nil, err
		}
		return netpkg.QuicStreamToNetConn(stream, c.quicConn), nil
	}

	if lo.FromPtr(c.cfg.Transport.TCPMux) {
		// First stream is reserved for the control connection for better stability and observability.
		if !c.firstConnectUsed.Swap(true) {
			return c.ConnectBySessionIndex(0)
		}
		return c.connectByBestSession()
	}

	return c.realConnect()
}

func (c *defaultConnectorImpl) MuxSessionCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.muxSessions)
}

func (c *defaultConnectorImpl) ConnectBySessionIndex(index int) (net.Conn, error) {
	if c.closed.Load() {
		return nil, errors.New("connector closed")
	}
	c.mu.RLock()
	if index < 0 || index >= len(c.muxSessions) {
		c.mu.RUnlock()
		return nil, fmt.Errorf("invalid mux session index %d", index)
	}
	session := c.muxSessions[index]
	stats := c.muxStats[index]
	c.mu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("mux session %d is nil", index)
	}

	start := time.Now()
	stream, err := session.OpenStream()
	openDur := time.Since(start)
	c.recordOpenStreamResult(stats, openDur, err)
	if err != nil {
		return nil, err
	}

	stats.activeStreams.Add(1)
	streamConn := netpkg.WrapCloseNotifyConn(stream, func(_ error) {
		stats.activeStreams.Add(-1)
	})
	return &muxStreamConn{
		Conn:            streamConn,
		muxSessionIndex: index,
	}, nil
}

func (c *defaultConnectorImpl) connectByBestSession() (net.Conn, error) {
	now := time.Now()

	c.mu.RLock()
	sessionCount := len(c.muxSessions)
	c.mu.RUnlock()
	if sessionCount == 0 {
		return nil, errors.New("no tcp mux session available")
	}

	// Warm-up: before we have any probe/latency signals, spread new streams across sessions
	// to seed the workConn pool and avoid concentrating on a single TCP connection.
	c.mu.RLock()
	anyWarm := false
	for i := 0; i < sessionCount; i++ {
		if c.muxSessions[i] == nil || c.muxStats[i] == nil {
			continue
		}
		if c.muxStats[i].warm() {
			anyWarm = true
			break
		}
	}
	c.mu.RUnlock()
	if !anyWarm {
		start := int(c.rrCounter.Add(1)-1) % sessionCount
		for i := 0; i < sessionCount; i++ {
			idx := (start + i) % sessionCount
			c.mu.RLock()
			stats := c.muxStats[idx]
			c.mu.RUnlock()
			if stats == nil || !stats.healthy(now) {
				continue
			}
			conn, err := c.ConnectBySessionIndex(idx)
			if err == nil {
				return conn, nil
			}
			// fall through to score-based selection if it fails
			break
		}
	}

	tried := make([]bool, sessionCount)
	var lastErr error
	for attempt := 0; attempt < sessionCount; attempt++ {
		best := -1
		bestScore := 1e18

		c.mu.RLock()
		for i := 0; i < sessionCount; i++ {
			if tried[i] || c.muxSessions[i] == nil {
				continue
			}
			score := c.muxStats[i].score(now)
			if score < bestScore {
				bestScore = score
				best = i
			}
		}
		c.mu.RUnlock()

		if best < 0 {
			break
		}
		tried[best] = true
		conn, err := c.ConnectBySessionIndex(best)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("failed to open stream on all mux sessions")
	}
	return nil, lastErr
}

func (c *defaultConnectorImpl) recordOpenStreamResult(stats *muxLinkStats, d time.Duration, err error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if err != nil {
		stats.errEWMA = ewmaUpdate(stats.errEWMA, 1, 0.2)
		stats.consecutiveErrors++
		backoff := time.Duration(stats.consecutiveErrors) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		stats.unhealthyUntil = time.Now().Add(backoff)
		return
	}
	stats.errEWMA = ewmaUpdate(stats.errEWMA, 0, 0.2)
	stats.consecutiveErrors = 0
	stats.unhealthyUntil = time.Time{}
	stats.openStreamEWMA = ewmaUpdate(stats.openStreamEWMA, float64(d.Nanoseconds()), 0.2)
}

func (c *defaultConnectorImpl) ReportMuxLinkProbeResult(index int, rtt time.Duration, err error) {
	c.mu.RLock()
	if index < 0 || index >= len(c.muxStats) {
		c.mu.RUnlock()
		return
	}
	stats := c.muxStats[index]
	c.mu.RUnlock()
	if stats == nil {
		return
	}

	stats.mu.Lock()
	defer stats.mu.Unlock()
	if err != nil {
		stats.errEWMA = ewmaUpdate(stats.errEWMA, 1, 0.1)
		stats.consecutiveErrors++
		backoff := time.Duration(stats.consecutiveErrors) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		stats.unhealthyUntil = time.Now().Add(backoff)
		return
	}
	stats.errEWMA = ewmaUpdate(stats.errEWMA, 0, 0.1)
	stats.consecutiveErrors = 0
	stats.unhealthyUntil = time.Time{}
	stats.rttEWMA = ewmaUpdate(stats.rttEWMA, float64(rtt.Nanoseconds()), 0.1)
}

func (c *defaultConnectorImpl) realConnect() (net.Conn, error) {
	xl := xlog.FromContextSafe(c.ctx)
	var tlsConfig *tls.Config
	var err error
	tlsEnable := lo.FromPtr(c.cfg.Transport.TLS.Enable)
	if c.cfg.Transport.Protocol == "wss" {
		tlsEnable = true
	}
	if tlsEnable {
		sn := c.cfg.Transport.TLS.ServerName
		if sn == "" {
			sn = c.cfg.ServerAddr
		}

		tlsConfig, err = transport.NewClientTLSConfig(
			c.cfg.Transport.TLS.CertFile,
			c.cfg.Transport.TLS.KeyFile,
			c.cfg.Transport.TLS.TrustedCaFile,
			sn)
		if err != nil {
			xl.Warnf("fail to build tls configuration, err: %v", err)
			return nil, err
		}
	}

	proxyType, addr, auth, err := libnet.ParseProxyURL(c.cfg.Transport.ProxyURL)
	if err != nil {
		xl.Errorf("fail to parse proxy url")
		return nil, err
	}
	dialOptions := []libnet.DialOption{}
	protocol := c.cfg.Transport.Protocol
	switch protocol {
	case "websocket":
		protocol = "tcp"
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{Hook: netpkg.DialHookWebsocket(protocol, "")}))
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{
			Hook: netpkg.DialHookCustomTLSHeadByte(tlsConfig != nil, lo.FromPtr(c.cfg.Transport.TLS.DisableCustomTLSFirstByte)),
		}))
		dialOptions = append(dialOptions, libnet.WithTLSConfig(tlsConfig))
	case "wss":
		protocol = "tcp"
		dialOptions = append(dialOptions, libnet.WithTLSConfigAndPriority(100, tlsConfig))
		// Make sure that if it is wss, the websocket hook is executed after the tls hook.
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{Hook: netpkg.DialHookWebsocket(protocol, tlsConfig.ServerName), Priority: 110}))
	default:
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{
			Hook: netpkg.DialHookCustomTLSHeadByte(tlsConfig != nil, lo.FromPtr(c.cfg.Transport.TLS.DisableCustomTLSFirstByte)),
		}))
		dialOptions = append(dialOptions, libnet.WithTLSConfig(tlsConfig))
	}

	if c.cfg.Transport.ConnectServerLocalIP != "" {
		dialOptions = append(dialOptions, libnet.WithLocalAddr(c.cfg.Transport.ConnectServerLocalIP))
	}
	dialOptions = append(dialOptions,
		libnet.WithProtocol(protocol),
		libnet.WithTimeout(time.Duration(c.cfg.Transport.DialServerTimeout)*time.Second),
		libnet.WithKeepAlive(time.Duration(c.cfg.Transport.DialServerKeepAlive)*time.Second),
		libnet.WithProxy(proxyType, addr),
		libnet.WithProxyAuth(auth),
	)
	conn, err := libnet.DialContext(
		c.ctx,
		net.JoinHostPort(c.cfg.ServerAddr, strconv.Itoa(c.cfg.ServerPort)),
		dialOptions...,
	)
	return conn, err
}

func (c *defaultConnectorImpl) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		if c.quicConn != nil {
			_ = c.quicConn.CloseWithError(0, "")
		}

		c.mu.Lock()
		for _, session := range c.muxSessions {
			if session != nil {
				_ = session.Close()
			}
		}
		c.muxSessions = nil
		c.muxStats = nil
		c.mu.Unlock()
	})
	return nil
}
