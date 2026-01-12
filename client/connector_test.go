package client

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	fmux "github.com/hashicorp/yamux"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func newYamuxPair(t *testing.T) (*fmux.Session, *fmux.Session) {
	t.Helper()

	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})

	cfg := fmux.DefaultConfig()
	cfg.LogOutput = io.Discard
	cfg.MaxStreamWindowSize = 1024 * 1024

	serverCh := make(chan *fmux.Session, 1)
	serverErrCh := make(chan error, 1)
	go func() {
		s, err := fmux.Server(c2, cfg)
		if err != nil {
			serverErrCh <- err
			return
		}
		serverCh <- s
	}()

	clientSess, err := fmux.Client(c1, cfg)
	require.NoError(t, err)

	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("failed to create yamux server session: %v", err)
		}
		return nil, nil
	case serverSess := <-serverCh:
		t.Cleanup(func() {
			_ = clientSess.Close()
			_ = serverSess.Close()
		})
		return clientSess, serverSess
	case <-time.After(2 * time.Second):
		t.Fatal("timeout creating yamux server session")
		return nil, nil
	}
	return nil, nil
}

func startEchoServer(t *testing.T, serverSess *fmux.Session) {
	t.Helper()
	go func() {
		for {
			s, err := serverSess.AcceptStream()
			if err != nil {
				return
			}
			go func(stream net.Conn) {
				defer stream.Close()
				_ = stream.SetDeadline(time.Now().Add(5 * time.Second))
				var header [4]byte
				if _, err := io.ReadFull(stream, header[:]); err != nil {
					return
				}
				n := binary.BigEndian.Uint32(header[:])
				buf := make([]byte, n)
				if _, err := io.ReadFull(stream, buf); err != nil {
					return
				}
				var outHeader [4]byte
				binary.BigEndian.PutUint32(outHeader[:], uint32(len(buf)))
				_, _ = stream.Write(outHeader[:])
				_, _ = stream.Write(buf)
			}(s)
		}
	}()
}

func roundTripPayload(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	_, err := conn.Write(header[:])
	require.NoError(t, err)
	_, err = conn.Write(payload)
	require.NoError(t, err)

	var respHeader [4]byte
	_, err = io.ReadFull(conn, respHeader[:])
	require.NoError(t, err)
	n := binary.BigEndian.Uint32(respHeader[:])
	resp := make([]byte, n)
	_, err = io.ReadFull(conn, resp)
	require.NoError(t, err)
	require.Equal(t, payload, resp)
}

func TestConnector_MultiMuxSession_SwitchingIsPayloadTransparent(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	sess0, srv0 := newYamuxPair(t)
	sess1, srv1 := newYamuxPair(t)
	startEchoServer(t, srv0)
	startEchoServer(t, srv1)

	cfg := &v1.ClientCommonConfig{}
	cfg.Transport.TCPMux = lo.ToPtr(true)

	c := &defaultConnectorImpl{
		ctx: context.Background(),
		cfg: cfg,
	}
	c.muxSessions = []*fmux.Session{sess0, sess1}
	c.muxStats = []*muxLinkStats{&muxLinkStats{}, &muxLinkStats{}}
	c.firstConnectUsed.Store(true)

	// Prefer session 0 initially.
	c.ReportMuxLinkProbeResult(0, 10*time.Millisecond, nil)
	c.ReportMuxLinkProbeResult(1, 50*time.Millisecond, nil)

	conn, err := c.Connect()
	require.NoError(err)
	idx := conn.(interface{ MuxSessionIndex() int }).MuxSessionIndex()
	require.Equal(0, idx)
	roundTripPayload(t, conn, []byte("hello-session-0"))
	require.NoError(conn.Close())

	// Switch preference to session 1.
	// Probe results are smoothed by EWMA (alpha=0.1), so a single "better" sample on
	// session 1 is not enough to flip the ranking immediately.
	c.ReportMuxLinkProbeResult(0, 200*time.Millisecond, nil)
	c.ReportMuxLinkProbeResult(1, 20*time.Millisecond, nil)
	conn, err = c.Connect()
	require.NoError(err)
	idx = conn.(interface{ MuxSessionIndex() int }).MuxSessionIndex()
	require.Equal(0, idx)
	roundTripPayload(t, conn, []byte("still-session-0-after-1-sample"))
	require.NoError(conn.Close())

	// Provide more samples to ensure the ranking converges and selection switches.
	for i := 0; i < 4; i++ {
		c.ReportMuxLinkProbeResult(0, 200*time.Millisecond, nil)
		c.ReportMuxLinkProbeResult(1, 20*time.Millisecond, nil)
	}

	conn, err = c.Connect()
	require.NoError(err)
	idx = conn.(interface{ MuxSessionIndex() int }).MuxSessionIndex()
	require.Equal(1, idx)
	roundTripPayload(t, conn, []byte("hello-session-1"))
	require.NoError(conn.Close())
}

func TestConnector_MultiMuxSession_ActiveStreamsAffectSelection(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	sess0, srv0 := newYamuxPair(t)
	sess1, srv1 := newYamuxPair(t)
	startEchoServer(t, srv0)
	startEchoServer(t, srv1)

	cfg := &v1.ClientCommonConfig{}
	cfg.Transport.TCPMux = lo.ToPtr(true)

	c := &defaultConnectorImpl{
		ctx: context.Background(),
		cfg: cfg,
	}
	c.muxSessions = []*fmux.Session{sess0, sess1}
	c.muxStats = []*muxLinkStats{&muxLinkStats{}, &muxLinkStats{}}
	c.firstConnectUsed.Store(true)

	// Same quality signal for both sessions.
	c.ReportMuxLinkProbeResult(0, 10*time.Millisecond, nil)
	c.ReportMuxLinkProbeResult(1, 10*time.Millisecond, nil)

	// Keep one stream open on session 0 to increase activeStreams penalty.
	hold, err := c.ConnectBySessionIndex(0)
	require.NoError(err)
	t.Cleanup(func() { _ = hold.Close() })

	conn, err := c.Connect()
	require.NoError(err)
	idx := conn.(interface{ MuxSessionIndex() int }).MuxSessionIndex()
	require.Equal(1, idx)
	roundTripPayload(t, conn, []byte("active-streams-selection"))
}
