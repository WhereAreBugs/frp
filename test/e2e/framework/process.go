package framework

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	flog "github.com/fatedier/frp/pkg/util/log"
	"github.com/fatedier/frp/test/e2e/pkg/process"
)

// RunProcesses run multiple processes from templates.
// The first template should always be frps.
func (f *Framework) RunProcesses(serverTemplates []string, clientTemplates []string) ([]*process.Process, []*process.Process) {
	templates := make([]string, 0, len(serverTemplates)+len(clientTemplates))
	templates = append(templates, serverTemplates...)
	templates = append(templates, clientTemplates...)
	outs, ports, err := f.RenderTemplates(templates)
	ExpectNoError(err)
	ExpectTrue(len(templates) > 0)

	for name, port := range ports {
		f.usedPorts[name] = port
	}

	currentServerProcesses := make([]*process.Process, 0, len(serverTemplates))
	for i := range serverTemplates {
		confText := outs[i]
		path := filepath.Join(f.TempDirectory, fmt.Sprintf("frp-e2e-server-%d", i))
		err = os.WriteFile(path, []byte(confText), 0o600)
		ExpectNoError(err)

		if TestContext.Debug {
			flog.Debugf("[%s] %s", path, confText)
		}

		p := process.NewWithEnvs(TestContext.FRPServerPath, []string{"-c", path}, f.osEnvs)
		f.serverConfPaths = append(f.serverConfPaths, path)
		f.serverProcesses = append(f.serverProcesses, p)
		currentServerProcesses = append(currentServerProcesses, p)
		err = p.Start()
		ExpectNoError(err)

		bindAddr, bindPort, ok := parseBindAddrPort(confText)
		if ok {
			if len(clientTemplates) == 0 {
				// Server-only runs are used by tests that expect server startup failures.
				// Use a normal panic (instead of ginkgo Fail) so tests can recover when desired.
				if err := waitForTCPListen(bindAddr, bindPort, 2500*time.Millisecond); err != nil {
					panic(fmt.Sprintf("wait for frps listen (addr=%s port=%d): %v\nstderr=%s\nstdout=%s", bindAddr, bindPort, err, p.ErrorOutput(), p.StdOutput()))
				}
			} else {
				// With clients, try best-effort readiness probing to reduce flakiness.
				// If it still doesn't become ready in time, fall back to the original behavior.
				if err := waitForTCPListen(bindAddr, bindPort, 10*time.Second); err != nil {
					flog.Debugf("wait for frps listen timeout (addr=%s port=%d): %v\nstderr=%s\nstdout=%s", bindAddr, bindPort, err, p.ErrorOutput(), p.StdOutput())
				}
			}
		} else {
			// Fallback to original behavior when bindPort cannot be parsed.
			time.Sleep(500 * time.Millisecond)
		}
	}

	currentClientProcesses := make([]*process.Process, 0, len(clientTemplates))
	for i := range clientTemplates {
		index := i + len(serverTemplates)
		path := filepath.Join(f.TempDirectory, fmt.Sprintf("frp-e2e-client-%d", i))
		err = os.WriteFile(path, []byte(outs[index]), 0o600)
		ExpectNoError(err)

		if TestContext.Debug {
			flog.Debugf("[%s] %s", path, outs[index])
		}

		p := process.NewWithEnvs(TestContext.FRPClientPath, []string{"-c", path}, f.osEnvs)
		f.clientConfPaths = append(f.clientConfPaths, path)
		f.clientProcesses = append(f.clientProcesses, p)
		currentClientProcesses = append(currentClientProcesses, p)
		err = p.Start()
		ExpectNoError(err)
		time.Sleep(200 * time.Millisecond)
	}
	// Give frpc a bit time to register proxies before first request.
	time.Sleep(3 * time.Second)

	return currentServerProcesses, currentClientProcesses
}

func (f *Framework) RunFrps(args ...string) (*process.Process, string, error) {
	p := process.NewWithEnvs(TestContext.FRPServerPath, args, f.osEnvs)
	f.serverProcesses = append(f.serverProcesses, p)
	err := p.Start()
	if err != nil {
		return p, p.StdOutput(), err
	}
	// Give frps extra time to finish binding ports before proceeding.
	time.Sleep(4 * time.Second)
	return p, p.StdOutput(), nil
}

func (f *Framework) RunFrpc(args ...string) (*process.Process, string, error) {
	p := process.NewWithEnvs(TestContext.FRPClientPath, args, f.osEnvs)
	f.clientProcesses = append(f.clientProcesses, p)
	err := p.Start()
	if err != nil {
		return p, p.StdOutput(), err
	}
	time.Sleep(2 * time.Second)
	return p, p.StdOutput(), nil
}

func (f *Framework) GenerateConfigFile(content string) string {
	f.configFileIndex++
	path := filepath.Join(f.TempDirectory, fmt.Sprintf("frp-e2e-config-%d", f.configFileIndex))
	err := os.WriteFile(path, []byte(content), 0o600)
	ExpectNoError(err)
	return path
}

func parseBindAddrPort(confText string) (bindAddr string, bindPort int, ok bool) {
	// Default to loopback for readiness checks.
	bindAddr = "127.0.0.1"

	scanner := bufio.NewScanner(strings.NewReader(confText))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// TOML/INI comments.
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// v1 (TOML/YAML rendered as TOML in tests): bindAddr / bindPort
		if strings.HasPrefix(line, "bindAddr") {
			if v, ok := parseConfigValue(line); ok {
				bindAddr = strings.Trim(v, "\"")
			}
			continue
		}
		if strings.HasPrefix(line, "bindPort") {
			if v, ok := parseConfigValue(line); ok {
				if p, err := strconv.Atoi(v); err == nil {
					bindPort = p
				}
			}
			continue
		}

		// legacy ini: bind_addr / bind_port
		if strings.HasPrefix(line, "bind_addr") {
			if v, ok := parseConfigValue(line); ok {
				bindAddr = v
			}
			continue
		}
		if strings.HasPrefix(line, "bind_port") {
			if v, ok := parseConfigValue(line); ok {
				if p, err := strconv.Atoi(v); err == nil {
					bindPort = p
				}
			}
			continue
		}
	}

	if bindPort <= 0 {
		return "", 0, false
	}
	return bindAddr, bindPort, true
}

func parseConfigValue(line string) (string, bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", false
	}
	v := strings.TrimSpace(line[idx+1:])
	if v == "" {
		return "", false
	}
	// Strip inline comment.
	if i := strings.IndexAny(v, "#;"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

func waitForTCPListen(bindAddr string, bindPort int, timeout time.Duration) error {
	if bindPort <= 0 {
		return fmt.Errorf("invalid bindPort %d", bindPort)
	}

	addr := strings.TrimSpace(bindAddr)
	probeAddrs := []string{addr}
	if addr == "" || addr == "0.0.0.0" || addr == "::" {
		probeAddrs = []string{"127.0.0.1", "::1"}
	}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		for _, host := range probeAddrs {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(bindPort)), 200*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout waiting for %s:%d", bindAddr, bindPort)
	}
	return lastErr
}
