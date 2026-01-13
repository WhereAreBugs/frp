package client

import (
	"fmt"
	"slices"
	"strings"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

type ServerProtocolPort struct {
	Protocol string
	Port     int
}

// ServerProtocolPorts returns the list of protocol/port pairs to use for a server entry.
// If any protocol-specific port is configured, only those protocols will be used.
// Otherwise, the default transport protocol and server port will be returned.
func ServerProtocolPorts(common *v1.ClientCommonConfig, server v1.ClientServerConfig) []ServerProtocolPort {
	hasProtocolPorts := server.HasProtocolPorts()

	var out []ServerProtocolPort
	add := func(protocol string, port int) {
		if port <= 0 {
			return
		}
		out = append(out, ServerProtocolPort{
			Protocol: strings.ToLower(protocol),
			Port:     port,
		})
	}

	if hasProtocolPorts {
		add("tcp", server.TCPPort)
		add("quic", server.QUICPort)
		add("kcp", server.KCPPort)
		add("websocket", server.WebsocketPort)
		add("wss", server.WSSPort)
		return out
	}

	protocol := strings.ToLower(strings.TrimSpace(common.Transport.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	port := server.Port
	if port == 0 {
		port = common.ServerPort
	}
	add(protocol, port)
	return out
}

func FormatServerProtocolPorts(ports []ServerProtocolPort) string {
	if len(ports) == 0 {
		return ""
	}
	strs := make([]string, 0, len(ports))
	for _, p := range ports {
		strs = append(strs, fmt.Sprintf("%s:%d", p.Protocol, p.Port))
	}
	return strings.Join(strs, ",")
}

// FilterProxyCfgsByServerName filters proxy configurers by the server allow list.
// If a proxy has an empty allow list, it will be included for all servers.
func FilterProxyCfgsByServerName(cfgs []v1.ProxyConfigurer, serverName string) []v1.ProxyConfigurer {
	if len(cfgs) == 0 {
		return nil
	}
	out := make([]v1.ProxyConfigurer, 0, len(cfgs))
	for _, c := range cfgs {
		base := c.GetBaseConfig()
		if len(base.ServerNames) == 0 || slices.Contains(base.ServerNames, serverName) {
			out = append(out, c)
		}
	}
	return out
}

// FilterVisitorCfgsByServerName filters visitor configurers by the explicit frps binding.
//
// In multi-frps mode, visitors must bind to exactly one server by `frpsName`.
func FilterVisitorCfgsByServerName(cfgs []v1.VisitorConfigurer, serverName string) []v1.VisitorConfigurer {
	if len(cfgs) == 0 {
		return nil
	}
	out := make([]v1.VisitorConfigurer, 0, len(cfgs))
	for _, c := range cfgs {
		base := c.GetBaseConfig()
		if strings.TrimSpace(base.FRPServerName) == serverName {
			out = append(out, c)
		}
	}
	return out
}
