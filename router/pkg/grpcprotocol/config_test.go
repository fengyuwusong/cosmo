package grpcprotocol

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wundergraph/cosmo/router/pkg/config"
	grpcdatasource "github.com/wundergraph/graphql-go-tools/v2/pkg/engine/datasource/grpc_datasource"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name         string
		config       config.GRPCProtocolConfiguration
		wantProtocol Protocol
		wantEncoding grpcdatasource.ConnectEncoding
		wantError    string
	}{
		{name: "zero value defaults", wantProtocol: ProtocolGRPC, wantEncoding: grpcdatasource.ConnectEncodingProtobuf},
		{name: "connect json", config: config.GRPCProtocolConfiguration{DefaultProtocol: "connectrpc", ConnectRPCEncoding: "json"}, wantProtocol: ProtocolConnectRPC, wantEncoding: grpcdatasource.ConnectEncodingJSON},
		{name: "dormant encoding is valid", config: config.GRPCProtocolConfiguration{DefaultProtocol: "grpc", ConnectRPCEncoding: "json"}, wantProtocol: ProtocolGRPC, wantEncoding: grpcdatasource.ConnectEncodingJSON},
		{name: "invalid protocol", config: config.GRPCProtocolConfiguration{DefaultProtocol: "auto"}, wantError: "expected grpc or connectrpc"},
		{name: "invalid encoding", config: config.GRPCProtocolConfiguration{ConnectRPCEncoding: "yaml"}, wantError: "expected proto or json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.config)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantProtocol, got.Protocol)
			require.Equal(t, tt.wantEncoding, got.Encoding)
		})
	}
}

func TestValidateRoutingURL(t *testing.T) {
	tests := []struct {
		name      string
		protocol  Protocol
		url       string
		wantError string
	}{
		{name: "connect http", protocol: ProtocolConnectRPC, url: "http://service:8080"},
		{name: "connect https path and trailing slash", protocol: ProtocolConnectRPC, url: "https://service.example/rpc/"},
		{name: "connect rejects dns", protocol: ProtocolConnectRPC, url: "dns:///service:443", wantError: "absolute http:// or https://"},
		{name: "connect rejects empty host", protocol: ProtocolConnectRPC, url: "http:///rpc", wantError: "non-empty host"},
		{name: "connect rejects user info", protocol: ProtocolConnectRPC, url: "https://user@service/rpc", wantError: "user information"},
		{name: "connect rejects query", protocol: ProtocolConnectRPC, url: "https://service/rpc?x=1", wantError: "query parameters"},
		{name: "connect rejects fragment", protocol: ProtocolConnectRPC, url: "https://service/rpc#x", wantError: "fragments"},
		{name: "grpc accepts dns", protocol: ProtocolGRPC, url: "dns:///service:443"},
		{name: "grpc accepts bare target", protocol: ProtocolGRPC, url: "service:443"},
		{name: "grpc accepts host named http", protocol: ProtocolGRPC, url: "http:50051"},
		{name: "grpc accepts host named https", protocol: ProtocolGRPC, url: "https:50051"},
		{name: "grpc rejects http", protocol: ProtocolGRPC, url: "https://service:443", wantError: "gRPC resolver address"},
		{name: "grpc rejects case insensitive HTTP URL", protocol: ProtocolGRPC, url: "HTTP://service:50051", wantError: "gRPC resolver address"},
		{name: "grpc rejects malformed http target", protocol: ProtocolGRPC, url: "http://%", wantError: "gRPC resolver address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRoutingURL(tt.protocol, tt.url)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}
