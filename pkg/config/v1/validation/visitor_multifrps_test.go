package validation

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func TestValidateAllClientConfigRequiresVisitorFRPServerNameInMultiFrps(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{
		Servers: []v1.ClientServerConfig{{Name: "s1", Addr: "127.0.0.1", Port: 7000}},
	}
	require.NoError(common.Complete())

	visitor := &v1.STCPVisitorConfig{}
	visitor.Name = "v1"
	visitor.Type = "stcp"
	visitor.ServerName = "proxy"
	visitor.BindPort = 6000
	visitor.Complete(common)

	_, err := ValidateAllClientConfig(common, nil, []v1.VisitorConfigurer{visitor}, nil)
	require.Error(err)
	require.ErrorContains(err, "frpsName is required")
}

func TestValidateAllClientConfigRejectsUnknownVisitorFRPServerName(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{
		Servers: []v1.ClientServerConfig{{Name: "s1", Addr: "127.0.0.1", Port: 7000}},
	}
	require.NoError(common.Complete())

	visitor := &v1.STCPVisitorConfig{}
	visitor.Name = "v1"
	visitor.Type = "stcp"
	visitor.ServerName = "proxy"
	visitor.BindPort = 6000
	visitor.FRPServerName = "s2"
	visitor.Complete(common)

	_, err := ValidateAllClientConfig(common, nil, []v1.VisitorConfigurer{visitor}, nil)
	require.Error(err)
	require.ErrorContains(err, "not found in servers")
}

func TestValidateAllClientConfigAllowsVisitorFRPServerName(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{
		Servers: []v1.ClientServerConfig{{Name: "s1", Addr: "127.0.0.1", Port: 7000}},
	}
	require.NoError(common.Complete())

	visitor := &v1.STCPVisitorConfig{}
	visitor.Name = "v1"
	visitor.Type = "stcp"
	visitor.ServerName = "proxy"
	visitor.BindPort = 6000
	visitor.FRPServerName = "s1"
	visitor.Complete(common)

	_, err := ValidateAllClientConfig(common, nil, []v1.VisitorConfigurer{visitor}, nil)
	require.NoError(err)
}
