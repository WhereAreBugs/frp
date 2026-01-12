package net

import "net"

// TCPFastOpenOptions controls whether TCP Fast Open (TFO) should be enabled.
//
// When unsupported by the operating system, enabling TFO is best-effort and
// will be silently ignored.
//
// Queue is used on some platforms (e.g. Linux) as the maximum number of pending
// TFO requests. If Queue <= 0, a reasonable default will be used.
type TCPFastOpenOptions struct {
	Enable bool
	Queue  int
}

const defaultTCPFastOpenQueue = 1024

// ListenTCP creates a TCP listener and (optionally) enables TCP Fast Open.
//
// This function is best-effort: if enabling TFO fails or is not supported,
// it will still return a working listener.
func ListenTCP(address string, opts TCPFastOpenOptions) (net.Listener, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	if !opts.Enable {
		return ln, nil
	}
	queue := opts.Queue
	if queue <= 0 {
		queue = defaultTCPFastOpenQueue
	}
	_ = enableTCPFastOpen(ln, queue)
	return ln, nil
}
