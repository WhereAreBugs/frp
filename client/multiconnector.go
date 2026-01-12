package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/util/xlog"
)

type multiConnector struct {
	xl         *xlog.Logger
	connectors []connectorWithMeta
	rr         atomic.Uint32
}

type connectorWithMeta struct {
	Connector
	protocol  string
	port      int
	available bool
}

// NewMultiConnector creates a connector that load balances Connect() calls across
// multiple underlying connectors using round-robin selection.
func NewMultiConnector(ctx context.Context, base *v1.ClientCommonConfig, ports []ServerProtocolPort) Connector {
	connectors := make([]connectorWithMeta, 0, len(ports))
	for _, p := range ports {
		cfgCopy := *base
		cfgCopy.Transport.Protocol = p.Protocol
		cfgCopy.ServerPort = p.Port
		connectors = append(connectors, connectorWithMeta{
			Connector: NewConnector(ctx, &cfgCopy),
			protocol:  p.Protocol,
			port:      p.Port,
		})
	}
	return &multiConnector{
		xl:         xlog.FromContextSafe(ctx),
		connectors: connectors,
	}
}

func (c *multiConnector) Open() error {
	if len(c.connectors) == 0 {
		return errors.New("no connectors configured")
	}

	var errs []error
	success := 0
	for i := range c.connectors {
		if err := c.connectors[i].Open(); err != nil {
			errs = append(errs, fmt.Errorf("%s:%d: %w", c.connectors[i].protocol, c.connectors[i].port, err))
			continue
		}
		c.connectors[i].available = true
		success++
	}

	if success == 0 {
		return errors.Join(errs...)
	}
	if len(errs) > 0 {
		c.xl.Warnf("some connectors failed to open, will continue with available ones: %v", errors.Join(errs...))
	}
	return nil
}

func (c *multiConnector) Connect() (net.Conn, error) {
	total := len(c.connectors)
	if total == 0 {
		return nil, errors.New("no connectors configured")
	}

	start := int(c.rr.Add(1)-1) % total
	var errs []error
	for i := 0; i < total; i++ {
		idx := (start + i) % total
		ent := &c.connectors[idx]
		if !ent.available {
			continue
		}
		conn, err := ent.Connect()
		if err == nil {
			return conn, nil
		}
		errs = append(errs, fmt.Errorf("%s:%d: %w", ent.protocol, ent.port, err))
	}

	if len(errs) == 0 {
		return nil, errors.New("no available connector")
	}
	err := errors.Join(errs...)
	c.xl.Warnf("all connector attempts failed: %v", err)
	return nil, err
}

func (c *multiConnector) Close() error {
	for i := range c.connectors {
		_ = c.connectors[i].Close()
	}
	return nil
}
