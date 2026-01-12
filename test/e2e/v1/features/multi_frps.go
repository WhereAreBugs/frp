package features

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/pkg/port"
	"github.com/fatedier/frp/test/e2e/pkg/request"
)

var _ = ginkgo.Describe("[Feature: Multi-FRPS]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("Active-active register with per-server token and per-proxy server allow list", func() {
		s1BindPortName := port.GenName("Frps1")
		s2BindPortName := port.GenName("Frps2")
		s1WebPortName := port.GenName("Frps1Web")
		s2WebPortName := port.GenName("Frps2Web")

		// Use unique bindPort placeholders per frps instance (consts.DefaultServerConfig shares one placeholder).
		// Use webServer to query the actual remote port when remotePort=0 is used to avoid port conflicts between multiple frps on localhost.
		server1Conf := fmt.Sprintf(`
bindPort = {{ .%s }}
bindAddr = "127.0.0.1"
proxyBindAddr = "127.0.0.1"
webServer.port = {{ .%s }}
webServer.addr = "127.0.0.1"
log.level = "trace"
auth.token = "token-1"
`, s1BindPortName, s1WebPortName)
		server2Conf := fmt.Sprintf(`
bindPort = {{ .%s }}
bindAddr = "127.0.0.1"
proxyBindAddr = "127.0.0.1"
webServer.port = {{ .%s }}
webServer.addr = "127.0.0.1"
log.level = "trace"
auth.token = "token-2"
`, s2BindPortName, s2WebPortName)

		clientConf := fmt.Sprintf(`
serverAddr = "127.0.0.1"
serverPort = {{ .%s }}
loginFailExit = false
log.level = "trace"
auth.token = "token-1"

servers = [
  { name = "s1", addr = "127.0.0.1", port = {{ .%s }}, token = "token-1" },
  { name = "s2", addr = "127.0.0.1", port = {{ .%s }}, token = "token-2" },
]

[[proxies]]
name = "all"
type = "tcp"
localPort = {{ .%s }}
remotePort = 0

[[proxies]]
name = "s1-only"
type = "tcp"
serverNames = ["s1"]
localPort = {{ .%s }}
remotePort = 0
`,
			s1BindPortName,
			s1BindPortName,
			s2BindPortName,
			framework.TCPEchoServerPort,
			framework.TCPEchoServerPort,
		)

		f.RunProcesses([]string{server1Conf, server2Conf}, []string{clientConf})

		getTCPProxyRemotePort := func(apiPort int, proxyName string) int {
			apiReq := request.New().HTTP().HTTPParams("GET", "", "/api/proxy/tcp/"+proxyName, nil).
				Addr("127.0.0.1").Port(apiPort).
				Timeout(2 * time.Second)
			ret, err := apiReq.Do()
			framework.ExpectNoError(err, "query proxy api", proxyName)
			framework.ExpectEqual(ret.Code, 200, "query proxy api", proxyName, string(ret.Content))

			var resp struct {
				Conf struct {
					RemotePort int `json:"remotePort"`
				} `json:"conf"`
				Status string `json:"status"`
			}
			framework.ExpectNoError(json.Unmarshal(ret.Content, &resp), "unmarshal proxy api response", proxyName)
			framework.ExpectEqual(resp.Status, "online", "proxy should be online", proxyName)
			framework.ExpectTrue(resp.Conf.RemotePort > 0, "remotePort should be assigned", proxyName)
			return resp.Conf.RemotePort
		}

		s1WebPort := f.PortByName(s1WebPortName)
		s2WebPort := f.PortByName(s2WebPortName)

		var (
			s1AllPort    int
			s2AllPort    int
			s1S1OnlyPort int
		)
		framework.NewRequestExpect(f).Explain("wait all registered on s1").
			RequestModify(func(r *request.Request) {
				r.HTTP().HTTPPath("/api/proxy/tcp/all").Addr("127.0.0.1").Port(s1WebPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond, framework.ExpectResponseCode(200))
		s1AllPort = getTCPProxyRemotePort(s1WebPort, "all")

		framework.NewRequestExpect(f).Explain("wait all registered on s2").
			RequestModify(func(r *request.Request) {
				r.HTTP().HTTPPath("/api/proxy/tcp/all").Addr("127.0.0.1").Port(s2WebPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond, framework.ExpectResponseCode(200))
		s2AllPort = getTCPProxyRemotePort(s2WebPort, "all")

		framework.NewRequestExpect(f).Explain("wait s1-only registered on s1").
			RequestModify(func(r *request.Request) {
				r.HTTP().HTTPPath("/api/proxy/tcp/s1-only").Addr("127.0.0.1").Port(s1WebPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond, framework.ExpectResponseCode(200))
		s1S1OnlyPort = getTCPProxyRemotePort(s1WebPort, "s1-only")

		// "s1-only" should not be registered to server s2.
		framework.NewRequestExpect(f).Explain("s1-only should not exist on s2").
			RequestModify(func(r *request.Request) {
				r.HTTP().HTTPPath("/api/proxy/tcp/s1-only").Addr("127.0.0.1").Port(s2WebPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond, framework.ExpectResponseCode(404))

		// Verify data path.
		framework.NewRequestExpect(f).Explain("all via s1").
			RequestModify(func(r *request.Request) {
				r.TCP().Addr("127.0.0.1").Port(s1AllPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond)

		framework.NewRequestExpect(f).Explain("all via s2").
			RequestModify(func(r *request.Request) {
				r.TCP().Addr("127.0.0.1").Port(s2AllPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond)

		framework.NewRequestExpect(f).Explain("s1-only via s1").
			RequestModify(func(r *request.Request) {
				r.TCP().Addr("127.0.0.1").Port(s1S1OnlyPort)
			}).
			EnsureEventually(10*time.Second, 200*time.Millisecond)
	})
})
