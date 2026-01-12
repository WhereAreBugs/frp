package validation

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func TestValidateClientServersAllowsProtocolPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c := &v1.ClientCommonConfig{
		Servers: []v1.ClientServerConfig{{
			Name:    "s1",
			Addr:    "example.com",
			Port:    0,
			TCPPort: 7000,
		}},
	}

	require.NoError(validateClientServers(c))
}

func TestValidateClientServersRequiresAtLeastOnePort(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c := &v1.ClientCommonConfig{
		Servers: []v1.ClientServerConfig{{
			Name: "s1",
			Addr: "example.com",
		}},
	}

	err := validateClientServers(c)
	require.Error(err)
	require.ErrorContains(err, "must set at least one port")
}

func TestValidateClientServersValidatesProtocolPortRange(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c := &v1.ClientCommonConfig{
		Servers: []v1.ClientServerConfig{{
			Name:     "s1",
			Addr:     "example.com",
			QUICPort: 70000,
		}},
	}

	err := validateClientServers(c)
	require.Error(err)
	require.ErrorContains(err, "servers[0].quicPort")
	require.ErrorContains(err, "range 0..65535")
}
