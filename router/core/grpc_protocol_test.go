package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	nodev1 "github.com/wundergraph/cosmo/router/gen/proto/wg/cosmo/node/v1"
	rcontext "github.com/wundergraph/cosmo/router/internal/context"
	"github.com/wundergraph/cosmo/router/pkg/config"
	"github.com/wundergraph/cosmo/router/pkg/grpcprotocol"
	grpcdatasource "github.com/wundergraph/graphql-go-tools/v2/pkg/engine/datasource/grpc_datasource"
)

func TestValidateGRPCSubgraphRoutingURLs(t *testing.T) {
	t.Run("connect accepts effective HTTP override", func(t *testing.T) {
		routerConfig := grpcTestRouterConfig("products", "dns:///products:443", false)
		err := validateGRPCSubgraphRoutingURLs(
			routerConfig,
			grpcprotocol.ProtocolConnectRPC,
			config.OverrideRoutingURLConfiguration{},
			config.OverridesConfiguration{Subgraphs: map[string]config.SubgraphOverridesConfiguration{
				"products": {RoutingURL: "https://products.example/rpc/"},
			}},
		)
		require.NoError(t, err)
	})

	t.Run("aggregates base and feature failures", func(t *testing.T) {
		routerConfig := grpcTestRouterConfig("products", "dns:///products:443", false)
		routerConfig.FeatureFlagConfigs = &nodev1.FeatureFlagRouterExecutionConfigs{
			ConfigByFeatureFlagName: map[string]*nodev1.FeatureFlagRouterExecutionConfig{
				"beta": {
					EngineConfig: grpcTestRouterConfig("products", "dns:///products:443", false).EngineConfig,
					Subgraphs:    grpcTestRouterConfig("products", "dns:///products:443", false).Subgraphs,
				},
				"checkout": {
					EngineConfig: grpcTestRouterConfig("payments", "unix:///payments.sock", false).EngineConfig,
					Subgraphs:    grpcTestRouterConfig("payments", "unix:///payments.sock", false).Subgraphs,
				},
			},
		}

		err := validateGRPCSubgraphRoutingURLs(routerConfig, grpcprotocol.ProtocolConnectRPC, config.OverrideRoutingURLConfiguration{}, config.OverridesConfiguration{})
		require.Error(t, err)
		require.ErrorContains(t, err, `subgraph "products" (base graph, feature flag "beta")`)
		require.ErrorContains(t, err, `subgraph "payments" (feature flag "checkout")`)
		require.Equal(t, 1, strings.Count(err.Error(), `subgraph "products"`))
	})

	t.Run("native grpc rejects HTTP URLs", func(t *testing.T) {
		err := validateGRPCSubgraphRoutingURLs(grpcTestRouterConfig("products", "HTTPS://products.example", false), grpcprotocol.ProtocolGRPC, config.OverrideRoutingURLConfiguration{}, config.OverridesConfiguration{})
		require.ErrorContains(t, err, "gRPC resolver address")
		require.ErrorContains(t, err, `routing URL "HTTPS://products.example"`)
	})

	t.Run("router plugins are outside global protocol selection", func(t *testing.T) {
		err := validateGRPCSubgraphRoutingURLs(grpcTestRouterConfig("plugin", "dns:///plugin:443", true), grpcprotocol.ProtocolConnectRPC, config.OverrideRoutingURLConfiguration{}, config.OverridesConfiguration{})
		require.NoError(t, err)
	})
}

func TestConnectSubgraphConfigurations(t *testing.T) {
	remote := grpcTestRouterConfig("products", "https://products.example/rpc", false)
	secondRemote := grpcTestRouterConfig("payments", "https://payments.example/rpc", false)
	plugin := grpcTestRouterConfig("plugin", "dns:///plugin:443", true)
	remote.EngineConfig.DatasourceConfigurations = append(remote.EngineConfig.DatasourceConfigurations, secondRemote.EngineConfig.DatasourceConfigurations...)
	remote.EngineConfig.DatasourceConfigurations = append(remote.EngineConfig.DatasourceConfigurations, plugin.EngineConfig.DatasourceConfigurations...)
	subgraphs := []Subgraph{
		{Id: "products", Name: "products", UrlString: "https://override.example/rpc"},
		{Id: "payments", Name: "payments", UrlString: "https://payments.example/rpc"},
		{Id: "plugin", Name: "plugin", UrlString: "dns:///plugin:443"},
	}

	got := connectSubgraphConfigurations(remote.EngineConfig, subgraphs, grpcprotocol.ResolvedConfiguration{
		Protocol: grpcprotocol.ProtocolConnectRPC,
		Encoding: grpcdatasource.ConnectEncodingJSON,
	})

	require.Equal(t, map[string]ConnectSubgraphConfiguration{
		"products": {BaseURL: "https://override.example/rpc", Encoding: grpcdatasource.ConnectEncodingJSON},
		"payments": {BaseURL: "https://payments.example/rpc", Encoding: grpcdatasource.ConnectEncodingJSON},
	}, got)
	require.Nil(t, connectSubgraphConfigurations(remote.EngineConfig, subgraphs, grpcprotocol.ResolvedConfiguration{Protocol: grpcprotocol.ProtocolGRPC}))
}

func TestSubgraphIdentityHTTPClientUsesConfiguredClient(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "products", req.Context().Value(rcontext.CurrentSubgraphContextKey{}))
		require.Empty(t, req.Header.Get("X-WG-Subgraph"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://products.example/rpc/package.Service/Method", http.NoBody)
	require.NoError(t, err)
	response, err := (subgraphIdentityHTTPClient{subgraphName: "products", client: client}).Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, calls)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func grpcTestRouterConfig(name, routingURL string, plugin bool) *nodev1.RouterConfig {
	grpcConfig := &nodev1.GRPCConfiguration{}
	if plugin {
		grpcConfig.Plugin = &nodev1.PluginConfiguration{}
	}
	return &nodev1.RouterConfig{
		EngineConfig: &nodev1.EngineConfiguration{
			DatasourceConfigurations: []*nodev1.DataSourceConfiguration{
				{
					Id: name,
					CustomGraphql: &nodev1.DataSourceCustom_GraphQL{
						Grpc: grpcConfig,
					},
				},
			},
		},
		Subgraphs: []*nodev1.Subgraph{{Id: name, Name: name, RoutingUrl: routingURL}},
	}
}
