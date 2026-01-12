package validation

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func TestValidateServerConfigRejectsNegativeTFOQueue(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	cfg := &v1.ServerConfig{}
	require.NoError(cfg.Complete())
	cfg.Transport.TCPFastOpen = true
	cfg.Transport.TCPFastOpenQueue = -1

	v := NewConfigValidator(nil)
	_, err := v.ValidateServerConfig(cfg)
	require.Error(err)
	require.ErrorContains(err, "transport.tcpFastOpenQueue")
}
