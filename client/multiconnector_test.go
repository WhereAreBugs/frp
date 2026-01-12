package client

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/util/xlog"
)

type fakeConnector struct {
	openErr    error
	connectErr error
	connects   atomic.Int32
}

func (c *fakeConnector) Open() error {
	return c.openErr
}

func (c *fakeConnector) Connect() (net.Conn, error) {
	if c.connectErr != nil {
		return nil, c.connectErr
	}
	c.connects.Add(1)
	server, client := net.Pipe()
	_ = server.Close()
	return client, nil
}

func (c *fakeConnector) Close() error { return nil }

func TestMultiConnectorRoundRobin(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mc := &multiConnector{
		xl: xlog.New(),
		connectors: []connectorWithMeta{
			{Connector: &fakeConnector{}, protocol: "tcp", port: 7000},
			{Connector: &fakeConnector{}, protocol: "quic", port: 7001},
		},
	}
	require.NoError(mc.Open())

	conn1, err := mc.Connect()
	require.NoError(err)
	conn2, err := mc.Connect()
	require.NoError(err)
	conn1.Close()
	conn2.Close()

	require.EqualValues(1, mc.connectors[0].Connector.(*fakeConnector).connects.Load())
	require.EqualValues(1, mc.connectors[1].Connector.(*fakeConnector).connects.Load())
}

func TestMultiConnectorSkipFailedOpen(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mc := &multiConnector{
		xl: xlog.New(),
		connectors: []connectorWithMeta{
			{Connector: &fakeConnector{openErr: errors.New("boom")}, protocol: "tcp", port: 7000},
			{Connector: &fakeConnector{}, protocol: "quic", port: 7001},
		},
	}
	require.NoError(mc.Open())

	conn, err := mc.Connect()
	require.NoError(err)
	conn.Close()

	require.EqualValues(0, mc.connectors[0].Connector.(*fakeConnector).connects.Load())
	require.EqualValues(1, mc.connectors[1].Connector.(*fakeConnector).connects.Load())
}

func TestNewMultiConnectorBuildsConnectors(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	common := &v1.ClientCommonConfig{}
	require.NoError(common.Complete())

	mc := NewMultiConnector(
		context.Background(),
		common,
		[]ServerProtocolPort{
			{Protocol: "tcp", Port: 7000},
			{Protocol: "quic", Port: 7001},
		},
	).(*multiConnector)

	require.Len(mc.connectors, 2)
	require.Equal("tcp", mc.connectors[0].protocol)
	require.Equal(7000, mc.connectors[0].port)
	require.Equal("quic", mc.connectors[1].protocol)
	require.Equal(7001, mc.connectors[1].port)
}

func TestMultiConnectorAllFail(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mc := &multiConnector{
		xl: xlog.New(),
		connectors: []connectorWithMeta{
			{Connector: &fakeConnector{openErr: errors.New("boom")}, protocol: "tcp", port: 7000},
		},
	}
	require.Error(mc.Open())
}

func TestMultiConnectorNoAvailableConnector(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mc := &multiConnector{
		xl: xlog.New(),
		connectors: []connectorWithMeta{
			{Connector: &fakeConnector{}, protocol: "tcp", port: 7000},
		},
	}

	_, err := mc.Connect()
	require.ErrorContains(err, "no available connector")
}

func TestMultiConnectorAllConnectFail(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mc := &multiConnector{
		xl: xlog.New(),
		connectors: []connectorWithMeta{
			{Connector: &fakeConnector{connectErr: errors.New("boom")}, protocol: "tcp", port: 7000},
			{Connector: &fakeConnector{connectErr: errors.New("boom")}, protocol: "quic", port: 7001},
		},
	}
	require.NoError(mc.Open())

	_, err := mc.Connect()
	require.Error(err)
	require.ErrorContains(err, "tcp:7000")
	require.ErrorContains(err, "quic:7001")
	require.ErrorContains(err, "boom")

	require.EqualValues(0, mc.connectors[0].Connector.(*fakeConnector).connects.Load())
	require.EqualValues(0, mc.connectors[1].Connector.(*fakeConnector).connects.Load())
}
