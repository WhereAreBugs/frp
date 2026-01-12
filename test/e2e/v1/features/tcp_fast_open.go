package features

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/framework/consts"
)

var _ = ginkgo.Describe("[Feature: TCP Fast Open]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("Enable TFO on frps listeners", func() {
		// Capability check: if the test is executed with an old frps binary,
		// strict config parsing will fail with "unknown field tcpFastOpen".
		// In that case, skip this test to avoid false negatives.
		verifyPath := f.GenerateConfigFile(`
		bindPort = 0
		log.level = "trace"
		transport.tcpFastOpen = true
		transport.tcpFastOpenQueue = 1024
		`)
		verifyCmd := exec.Command(framework.TestContext.FRPServerPath, "verify", "-c", verifyPath, "--strict_config=true")
		var verifyOut bytes.Buffer
		verifyCmd.Stdout = &verifyOut
		verifyCmd.Stderr = &verifyOut
		if err := verifyCmd.Run(); err != nil {
			if strings.Contains(verifyOut.String(), "unknown field \"tcpFastOpen\"") {
				ginkgo.Skip("frps binary does not support transport.tcpFastOpen; rebuild bin/frps-ext and retry")
			}
			framework.ExpectNoError(err, verifyOut.String())
		}

		remotePort := f.AllocPort()

		serverConf := consts.DefaultServerConfig + `
		transport.tcpFastOpen = true
		transport.tcpFastOpenQueue = 1024
		`

		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
		[[proxies]]
		name = "tcp"
		type = "tcp"
		localPort = {{ .%s }}
		remotePort = %d
		`, framework.TCPEchoServerPort, remotePort)

		f.RunProcesses([]string{serverConf}, []string{clientConf})

		framework.NewRequestExpect(f).
			Explain("tcp proxy with tfo enabled").
			Port(remotePort).
			EnsureEventually(10*time.Second, 200*time.Millisecond)
	})
})
