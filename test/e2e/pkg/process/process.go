package process

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

type Process struct {
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	errorOutput *bytes.Buffer
	stdOutput   *bytes.Buffer

	beforeStopHandler func()
	stopped           bool
}

func New(path string, params []string) *Process {
	return NewWithEnvs(path, params, nil)
}

func NewWithEnvs(path string, params []string, envs []string) *Process {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, path, params...)
	cmd.Env = envs
	p := &Process{
		cmd:    cmd,
		cancel: cancel,
	}
	p.errorOutput = bytes.NewBufferString("")
	p.stdOutput = bytes.NewBufferString("")
	cmd.Stderr = p.errorOutput
	cmd.Stdout = p.stdOutput
	return p
}

func (p *Process) Start() error {
	return p.cmd.Start()
}

func (p *Process) Stop() error {
	if p.stopped {
		return nil
	}
	defer func() {
		p.stopped = true
	}()
	if p.beforeStopHandler != nil {
		p.beforeStopHandler()
	}
	p.cancel()
	return p.cmd.Wait()
}

// WaitTimeout waits for the process to exit without sending a signal.
// It is useful for tests that expect a process to fail fast on startup.
func (p *Process) WaitTimeout(timeout time.Duration) (err error, exited bool) {
	if p.stopped {
		return nil, true
	}
	ch := make(chan error, 1)
	go func() {
		ch <- p.cmd.Wait()
	}()

	select {
	case err := <-ch:
		p.stopped = true
		return err, true
	case <-time.After(timeout):
		return nil, false
	}
}

func (p *Process) ErrorOutput() string {
	return p.errorOutput.String()
}

func (p *Process) StdOutput() string {
	return p.stdOutput.String()
}

func (p *Process) SetBeforeStopHandler(fn func()) {
	p.beforeStopHandler = fn
}
