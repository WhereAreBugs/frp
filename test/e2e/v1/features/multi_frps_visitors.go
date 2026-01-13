package features

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/pkg/port"
)

var _ = ginkgo.Describe("[Feature: Multi-FRPS Visitors]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("STCP visitor must bind to a specific frps", func() {
		// Capability check: if the test is executed with an old frpc binary,
		// strict config parsing will fail with "unknown field frpsName".
		// In that case, skip this test to avoid false negatives.
		verifyPath := f.GenerateConfigFile(`
			serverAddr = "127.0.0.1"
			serverPort = 7000
			loginFailExit = false
			log.level = "trace"

			servers = [
			  { name = "s1", addr = "127.0.0.1", port = 7000 },
			]

			[[visitors]]
			name = "v"
			type = "stcp"
			serverName = "x"
			frpsName = "s1"
			secretKey = "abcdefg"
			bindPort = 6000
			`)
		verifyCmd := exec.Command(framework.TestContext.FRPClientPath, "verify", "-c", verifyPath, "--strict_config=true")
		var verifyOut bytes.Buffer
		verifyCmd.Stdout = &verifyOut
		verifyCmd.Stderr = &verifyOut
		if err := verifyCmd.Run(); err != nil {
			if strings.Contains(verifyOut.String(), "unknown field \"frpsName\"") {
				ginkgo.Skip("frpc binary does not support visitors.frpsName; rebuild bin/frpc-ext and retry")
			}
			framework.ExpectNoError(err, verifyOut.String())
		}

		s1BindPortName := port.GenName("Frps1")
		s2BindPortName := port.GenName("Frps2")
		visitorBindPortName := port.GenName("Visitor")

		server1Conf := fmt.Sprintf(`
bindPort = {{ .%s }}
bindAddr = "127.0.0.1"
proxyBindAddr = "127.0.0.1"
log.level = "trace"
auth.token = "token"
`, s1BindPortName)
		server2Conf := fmt.Sprintf(`
bindPort = {{ .%s }}
bindAddr = "127.0.0.1"
proxyBindAddr = "127.0.0.1"
log.level = "trace"
auth.token = "token"
`, s2BindPortName)

		clientConf := fmt.Sprintf(`
serverAddr = "127.0.0.1"
serverPort = {{ .%s }}
loginFailExit = false
log.level = "trace"
auth.token = "token"

servers = [
  { name = "s1", addr = "127.0.0.1", port = {{ .%s }}, token = "token" },
  { name = "s2", addr = "127.0.0.1", port = {{ .%s }}, token = "token" },
]

[[proxies]]
name = "stcp-test"
type = "stcp"
serverNames = ["s1"]
localPort = {{ .%s }}
secretKey = "abcdefg"
allowUsers = ["*"]

[[visitors]]
name = "stcp-test-visitor"
type = "stcp"
serverName = "stcp-test"
frpsName = "s1"
secretKey = "abcdefg"
bindPort = {{ .%s }}
`, s1BindPortName, s1BindPortName, s2BindPortName, framework.TCPEchoServerPort, visitorBindPortName)

		f.RunProcesses([]string{server1Conf, server2Conf}, []string{clientConf})

		framework.NewRequestExpect(f).
			Explain("stcp visitor via s1").
			PortName(visitorBindPortName).
			EnsureEventually(20*time.Second, 200*time.Millisecond)
	})
})
