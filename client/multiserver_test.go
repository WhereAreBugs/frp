package client

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func TestFilterProxyCfgsByServerName(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	p1 := &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name:        "all",
			Type:        "tcp",
			ServerNames: nil,
		},
	}
	p2 := &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name:        "cn-only",
			Type:        "tcp",
			ServerNames: []string{"cn"},
		},
	}
	p3 := &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name:        "us-only",
			Type:        "tcp",
			ServerNames: []string{"us"},
		},
	}

	cfgs := []v1.ProxyConfigurer{p1, p2, p3}
	cn := FilterProxyCfgsByServerName(cfgs, "cn")
	us := FilterProxyCfgsByServerName(cfgs, "us")

	require.Len(cn, 2)
	require.Equal("all", cn[0].GetBaseConfig().Name)
	require.Equal("cn-only", cn[1].GetBaseConfig().Name)

	require.Len(us, 2)
	require.Equal("all", us[0].GetBaseConfig().Name)
	require.Equal("us-only", us[1].GetBaseConfig().Name)
}
