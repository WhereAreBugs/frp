// Copyright 2023 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestClientConfigComplete(t *testing.T) {
	require := require.New(t)
	c := &ClientConfig{}
	err := c.Complete()
	require.NoError(err)

	require.EqualValues("token", c.Auth.Method)
	require.Equal(true, lo.FromPtr(c.Transport.TCPMux))
	require.Equal(1, c.Transport.TCPMuxSessionCount)
	require.EqualValues(0, c.Transport.TCPMuxLinkProbeInterval)
	require.EqualValues("passive", c.Transport.TCPMuxLinkProbeMode)
	require.Equal(true, lo.FromPtr(c.LoginFailExit))
	require.Equal(true, lo.FromPtr(c.Transport.TLS.Enable))
	require.Equal(true, lo.FromPtr(c.Transport.TLS.DisableCustomTLSFirstByte))
	require.NotEmpty(c.NatHoleSTUNServer)
}

func TestAuthClientConfig_Complete(t *testing.T) {
	require := require.New(t)
	cfg := &AuthClientConfig{}
	err := cfg.Complete()
	require.NoError(err)
	require.EqualValues("token", cfg.Method)
}

func TestClientServerConfigHasProtocolPorts(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	require.False((ClientServerConfig{}).HasProtocolPorts())
	require.True((ClientServerConfig{TCPPort: 7000}).HasProtocolPorts())
	require.True((ClientServerConfig{QUICPort: 7001}).HasProtocolPorts())
	require.True((ClientServerConfig{WSSPort: 7002}).HasProtocolPorts())
}

func TestClientCommonConfigCompleteProtocolPortsDoNotFillPort(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c := &ClientCommonConfig{
		Servers: []ClientServerConfig{{
			Addr:    "example.com",
			TCPPort: 7000,
		}},
	}
	require.NoError(c.Complete())

	require.Len(c.Servers, 1)
	require.Equal("default", c.Servers[0].Name)
	require.Equal("example.com", c.Servers[0].Addr)
	// Port is left untouched because protocol-specific ports are configured.
	require.Equal(0, c.Servers[0].Port)
}
