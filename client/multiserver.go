package client

import (
	"slices"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

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
