package features

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/pkg/plugin/server"
	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/framework/consts"
	pluginpkg "github.com/fatedier/frp/test/e2e/pkg/plugin"
)

var _ = ginkgo.Describe("[Feature: TCPMux Link Probe]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("Sends Ping as first message on new streams (active probe)", func() {
		localPort := f.AllocPort()

		var probePingCount atomic.Int64
		newFunc := func() *server.Request {
			var r server.Request
			r.Content = &server.PingContent{}
			return &r
		}
		handler := func(req *server.Request) *server.Response {
			var ret server.Response
			content := req.Content.(*server.PingContent)
			// Link probe pings are sent on new streams and handled before control registration,
			// so UserInfo is expected to be empty.
			if content.User.User == "" && content.User.RunID == "" {
				probePingCount.Add(1)
			}
			ret.Unchange = true
			return &ret
		}
		pluginServer := pluginpkg.NewHTTPPluginServer(localPort, newFunc, handler, nil)
		f.RunServer("", pluginServer)

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
[[httpPlugins]]
name = "probe"
addr = "127.0.0.1:%d"
path = "/handler"
ops = ["Ping"]
`, localPort)

		remotePort := f.AllocPort()
		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
[transport]
tcpMux = true
tcpMuxSessionCount = 2
tcpMuxLinkProbeMode = "active"
tcpMuxLinkProbeInterval = 1
tcpMuxLinkProbeTimeout = 1

[[proxies]]
name = "tcp"
type = "tcp"
localPort = {{ .%s }}
remotePort = %d
`, framework.TCPEchoServerPort, remotePort)

		f.RunProcesses([]string{serverConf}, []string{clientConf})

		framework.NewRequestExpect(f).Port(remotePort).EnsureEventually(10*time.Second, 200*time.Millisecond)

		// Probe runs once immediately and then periodically. Wait a bit and ensure we saw probe pings.
		time.Sleep(3 * time.Second)
		framework.ExpectTrue(probePingCount.Load() >= 2)
	})
})
