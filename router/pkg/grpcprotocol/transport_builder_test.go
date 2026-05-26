package grpcprotocol

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wundergraph/cosmo/router/pkg/config"
)

func TestBuildConnectTransports_NilConfig(t *testing.T) {
	result, err := BuildConnectTransports(nil, map[string]string{"rpc": "http://localhost"}, nil, http.DefaultClient)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildConnectTransports_AllGRPC(t *testing.T) {
	cfg := &config.GRPCProtocolConfiguration{DefaultProtocol: ProtocolGRPC}
	urls := map[string]string{"rpc-a": "http://localhost:3000"}
	result, err := BuildConnectTransports(cfg, urls, nil, http.DefaultClient)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildConnectTransports_ConnectSubgraph(t *testing.T) {
	cfg := &config.GRPCProtocolConfiguration{DefaultProtocol: ProtocolConnectRPC}
	urls := map[string]string{"rpc-a": "http://localhost:3000"}
	result, err := BuildConnectTransports(cfg, urls, nil, http.DefaultClient)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result, "rpc-a")
}

func TestBuildConnectTransports_MixedProtocols(t *testing.T) {
	cfg := &config.GRPCProtocolConfiguration{
		DefaultProtocol: ProtocolGRPC,
		Subgraphs: map[string]config.GRPCProtocolSubgraph{
			"rpc-connect": {Protocol: ProtocolConnectRPC},
		},
	}
	urls := map[string]string{
		"rpc-grpc":    "http://localhost:3001",
		"rpc-connect": "http://localhost:3002",
	}
	result, err := BuildConnectTransports(cfg, urls, nil, http.DefaultClient)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result, "rpc-connect")
	assert.NotContains(t, result, "rpc-grpc")
}

func TestBuildConnectTransports_UsesPerSubgraphHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	cfg := &config.GRPCProtocolConfiguration{DefaultProtocol: ProtocolConnectRPC}
	urls := map[string]string{"rpc-a": "http://localhost:3000"}
	sgClients := map[string]*http.Client{"rpc-a": customClient}

	result, err := BuildConnectTransports(cfg, urls, sgClients, http.DefaultClient)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result, "rpc-a")
}

func TestBuildConnectTransports_EmptyURLs(t *testing.T) {
	cfg := &config.GRPCProtocolConfiguration{DefaultProtocol: ProtocolConnectRPC}
	result, err := BuildConnectTransports(cfg, map[string]string{}, nil, http.DefaultClient)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestBuildConnectTransports_RejectsNonTCPSchemes pins down that ConnectRPC
// subgraphs registered with a non-TCP routing URL (unix domain socket,
// vsock) are rejected at startup rather than silently translated into a
// broken http URL that the standard *http.Client cannot dial.
func TestBuildConnectTransports_RejectsNonTCPSchemes(t *testing.T) {
	cfg := &config.GRPCProtocolConfiguration{DefaultProtocol: ProtocolConnectRPC}
	tests := []struct {
		name string
		url  string
	}{
		{name: "unix", url: "unix:///tmp/grpc.sock"},
		{name: "unix-abstract", url: "unix-abstract:my-socket"},
		{name: "vsock", url: "vsock:2:1024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildConnectTransports(cfg, map[string]string{"rpc": tt.url}, nil, http.DefaultClient)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "connectrpc transport cannot dial")
		})
	}
}

func TestNormalizeConnectBaseURL(t *testing.T) {
	// The federated graph stores routing URLs in whichever scheme the user
	// registered them with: gRPC name resolver schemes (dns:///, ipv4://...),
	// the bare host:port form, or http(s):// for ConnectRPC. The Connect
	// HTTP client only understands http(s)://, so this test pins down the
	// translation between the two worlds.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty input passes through", in: "", want: ""},
		{name: "http URL pass-through", in: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "https URL pass-through", in: "https://api.example.com:8443/v1", want: "https://api.example.com:8443/v1"},
		{name: "dns scheme triple slash", in: "dns:///localhost:8080", want: "http://localhost:8080"},
		{name: "dns scheme with authority host:port", in: "dns://8.8.8.8/example.com:9000", want: "http://example.com:9000"},
		{name: "dns scheme single colon (no slashes)", in: "dns:localhost:8080", want: "http://localhost:8080"},
		{name: "plain host:port (defaults to dns)", in: "localhost:8080", want: "http://localhost:8080"},
		{name: "ipv4 scheme single endpoint", in: "ipv4:127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "passthrough scheme", in: "passthrough:///localhost:9000", want: "http://localhost:9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeConnectBaseURL(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNormalizeConnectBaseURL_RejectsNonTCPSchemes complements the table
// test above: unix-domain and vsock endpoints cannot be reached through
// the standard HTTP client, so the function reports an error instead of
// producing a misleading http URL.
func TestNormalizeConnectBaseURL_RejectsNonTCPSchemes(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "unix", in: "unix:///tmp/grpc.sock"},
		{name: "unix-abstract", in: "unix-abstract:my-socket"},
		{name: "vsock", in: "vsock:2:1024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeConnectBaseURL(tt.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot dial")
		})
	}
}
