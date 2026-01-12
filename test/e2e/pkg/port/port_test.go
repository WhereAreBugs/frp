package port

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocatorSkipsInUsePort(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	// Limit allocator range to the exact port so it must either pick it or fail.
	pa := NewAllocator(port, port, 1, 0)
	require.Equal(0, pa.Get())
}
