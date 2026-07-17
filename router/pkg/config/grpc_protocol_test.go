package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGRPCProtocolConfiguration(t *testing.T) {
	t.Run("defaults to native grpc and protobuf", func(t *testing.T) {
		f := createTempFileFromFixture(t, "version: '1'\n")

		cfg, err := LoadConfig([]string{f})
		require.NoError(t, err)
		require.Equal(t, "grpc", cfg.Config.GRPCProtocol.DefaultProtocol)
		require.Equal(t, "proto", cfg.Config.GRPCProtocol.ConnectRPCEncoding)
	})

	t.Run("loads yaml values", func(t *testing.T) {
		f := createTempFileFromFixture(t, `
version: '1'
grpc_protocol:
  default_protocol: connectrpc
  connectrpc_encoding: json
`)

		cfg, err := LoadConfig([]string{f})
		require.NoError(t, err)
		require.Equal(t, "connectrpc", cfg.Config.GRPCProtocol.DefaultProtocol)
		require.Equal(t, "json", cfg.Config.GRPCProtocol.ConnectRPCEncoding)
	})

	t.Run("loads environment values", func(t *testing.T) {
		t.Setenv("GRPC_PROTOCOL_DEFAULT_PROTOCOL", "connectrpc")
		t.Setenv("GRPC_PROTOCOL_CONNECTRPC_ENCODING", "json")
		f := createTempFileFromFixture(t, "version: '1'\n")

		cfg, err := LoadConfig([]string{f})
		require.NoError(t, err)
		require.Equal(t, "connectrpc", cfg.Config.GRPCProtocol.DefaultProtocol)
		require.Equal(t, "json", cfg.Config.GRPCProtocol.ConnectRPCEncoding)
	})

	t.Run("rejects invalid yaml values", func(t *testing.T) {
		f := createTempFileFromFixture(t, `
version: '1'
grpc_protocol:
  default_protocol: auto
  connectrpc_encoding: yaml
`)

		_, err := LoadConfig([]string{f})
		require.Error(t, err)
		require.ErrorContains(t, err, "must be one of")
	})

	t.Run("rejects invalid environment values", func(t *testing.T) {
		t.Setenv("GRPC_PROTOCOL_DEFAULT_PROTOCOL", "auto")
		t.Setenv("GRPC_PROTOCOL_CONNECTRPC_ENCODING", "yaml")
		f := createTempFileFromFixture(t, "version: '1'\n")

		_, err := LoadConfig([]string{f})
		require.Error(t, err)
		require.ErrorContains(t, err, "must be one of")
	})

	t.Run("rejects unsupported fields", func(t *testing.T) {
		f := createTempFileFromFixture(t, `
version: '1'
grpc_protocol:
  subgraphs:
    products:
      protocol: connectrpc
`)

		_, err := LoadConfig([]string{f})
		require.Error(t, err)
		require.ErrorContains(t, err, "additional properties 'subgraphs' not allowed")
	})
}
