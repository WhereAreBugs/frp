//go:build !(linux || darwin || freebsd)

package net

import "net"

func enableTCPFastOpen(_ net.Listener, _ int) error {
	return nil
}
