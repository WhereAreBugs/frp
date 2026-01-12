package net

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListenTCPWithFastOpenDoesNotFail(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	ln, err := ListenTCP("127.0.0.1:0", TCPFastOpenOptions{Enable: true, Queue: 1})
	require.NoError(err)
	require.NoError(ln.Close())
}
