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

func TestServerProtocolPortsDefault(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{}
	require.NoError(common.Complete())

	server := v1.ClientServerConfig{
		Name: "default",
		Addr: "example.com",
		Port: 7100,
	}

	ports := ServerProtocolPorts(common, server)
	require.Equal([]ServerProtocolPort{
		{Protocol: "tcp", Port: 7100},
	}, ports)
}

func TestServerProtocolPortsMultiProtocol(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{}
	require.NoError(common.Complete())

	server := v1.ClientServerConfig{
		Name:          "multi",
		Addr:          "example.com",
		TCPPort:       7000,
		QUICPort:      7001,
		KCPPort:       7002,
		WSSPort:       7003,
		WebsocketPort: 7004,
	}

	ports := ServerProtocolPorts(common, server)
	require.Equal([]ServerProtocolPort{
		{Protocol: "tcp", Port: 7000},
		{Protocol: "quic", Port: 7001},
		{Protocol: "kcp", Port: 7002},
		{Protocol: "websocket", Port: 7004},
		{Protocol: "wss", Port: 7003},
	}, ports)
}

func TestServerProtocolPortsExplicitOnly(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{}
	require.NoError(common.Complete())

	server := v1.ClientServerConfig{
		Name:     "quic-only",
		Addr:     "example.com",
		QUICPort: 7001,
	}

	ports := ServerProtocolPorts(common, server)
	require.Equal([]ServerProtocolPort{
		{Protocol: "quic", Port: 7001},
	}, ports)
}

func TestServerProtocolPortsFallbackToCommon(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{}
	require.NoError(common.Complete())
	common.Transport.Protocol = " QUIC "

	server := v1.ClientServerConfig{
		Name: "fallback",
		Addr: "example.com",
		Port: 0,
	}

	ports := ServerProtocolPorts(common, server)
	require.Equal([]ServerProtocolPort{
		{Protocol: "quic", Port: common.ServerPort},
	}, ports)
}

func TestFormatServerProtocolPorts(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	require.Equal("", FormatServerProtocolPorts(nil))
	require.Equal("", FormatServerProtocolPorts([]ServerProtocolPort{}))

	require.Equal(
		"tcp:7000,quic:7001",
		FormatServerProtocolPorts([]ServerProtocolPort{{Protocol: "tcp", Port: 7000}, {Protocol: "quic", Port: 7001}}),
	)
}
