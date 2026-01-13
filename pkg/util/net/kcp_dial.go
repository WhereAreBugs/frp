package net

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

var kcpDialConv uint32 = 100

func nextKCPConv() uint32 {
	return atomic.AddUint32(&kcpDialConv, 1)
}

// DialKCPContext dials a KCP connection with context/timeout support.
//
// Note: golib/net's built-in KCP dialer ignores timeout/context. This helper
// dials UDP with DialContext first to ensure we can return promptly.
func DialKCPContext(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{}
	udpConn, err := dialer.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}

	uc, ok := udpConn.(*net.UDPConn)
	if !ok {
		_ = udpConn.Close()
		return nil, &net.OpError{Op: "dial", Net: "udp", Err: err}
	}

	var pConn net.PacketConn = &ConnectedUDPConn{uc}
	kcpConn, err := kcp.NewConn3(nextKCPConv(), udpAddr, nil, 10, 3, pConn)
	if err != nil {
		_ = uc.Close()
		return nil, err
	}

	kcpConn.SetStreamMode(true)
	kcpConn.SetWriteDelay(true)
	kcpConn.SetNoDelay(1, 20, 2, 1)
	kcpConn.SetWindowSize(128, 512)
	kcpConn.SetMtu(1350)
	kcpConn.SetACKNoDelay(false)
	_ = kcpConn.SetReadBuffer(4194304)
	_ = kcpConn.SetWriteBuffer(4194304)
	return kcpConn, nil
}
