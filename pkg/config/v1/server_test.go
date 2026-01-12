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

func TestServerConfigComplete(t *testing.T) {
	require := require.New(t)
	c := &ServerConfig{}
	err := c.Complete()
	require.NoError(err)

	require.EqualValues("token", c.Auth.Method)
	require.Equal(true, lo.FromPtr(c.Transport.TCPMux))
	require.False(c.Transport.TCPFastOpen)
	require.Equal(0, c.Transport.TCPFastOpenQueue)
	require.Equal(true, lo.FromPtr(c.DetailedErrorsToClient))
}

func TestAuthServerConfig_Complete(t *testing.T) {
	require := require.New(t)
	cfg := &AuthServerConfig{}
	err := cfg.Complete()
	require.NoError(err)
	require.EqualValues("token", cfg.Method)
}

func TestServerTransportConfigCompleteSetsDefaultTFOQueue(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c := &ServerConfig{}
	require.NoError(c.Complete())

	c.Transport.TCPFastOpen = true
	c.Transport.TCPFastOpenQueue = 0
	c.Transport.Complete()
	require.Equal(1024, c.Transport.TCPFastOpenQueue)
}
