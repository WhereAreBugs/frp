//go:build linux || darwin || freebsd

package net

import (
	"net"

	"golang.org/x/sys/unix"
)

func enableTCPFastOpen(ln net.Listener, queue int) error {
	tl, ok := ln.(*net.TCPListener)
	if !ok {
		return nil
	}

	raw, err := tl.SyscallConn()
	if err != nil {
		return nil
	}

	var sockErr error
	err = raw.Control(func(fd uintptr) {
		// Best-effort: ignore any error so listener still works.
		sockErr = setSockoptTCPFastOpen(int(fd), queue)
	})
	if err != nil {
		return nil
	}
	return sockErr
}

func setSockoptTCPFastOpen(fd int, queue int) error {
	// Linux uses queue length; BSD/Darwin accept 0/1, but also tolerate positive ints.
	if queue <= 0 {
		queue = 1
	}
	return unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_FASTOPEN, queue)
}
