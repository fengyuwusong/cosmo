package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	nodev1 "github.com/wundergraph/cosmo/router/gen/proto/wg/cosmo/node/v1"
	"github.com/wundergraph/cosmo/router/pkg/config"
	"github.com/wundergraph/cosmo/router/pkg/grpcprotocol"
	grpcdatasource "github.com/wundergraph/graphql-go-tools/v2/pkg/engine/datasource/grpc_datasource"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

	t.Run("feature subgraphs require overrides under their actual names", func(t *testing.T) {
		routerConfig := grpcTestRouterConfig("products", "dns:///products:443", false)
		featureConfig := grpcTestRouterConfig("products-feature", "dns:///products-feature:443", false)
		routerConfig.FeatureFlagConfigs = &nodev1.FeatureFlagRouterExecutionConfigs{
			ConfigByFeatureFlagName: map[string]*nodev1.FeatureFlagRouterExecutionConfig{
				"beta": {
					EngineConfig: featureConfig.EngineConfig,
					Subgraphs:    featureConfig.Subgraphs,
				},
			},
		}

		err := validateGRPCSubgraphRoutingURLs(
			routerConfig,
			grpcprotocol.ProtocolConnectRPC,
			config.OverrideRoutingURLConfiguration{},
			config.OverridesConfiguration{Subgraphs: map[string]config.SubgraphOverridesConfiguration{
				"products": {RoutingURL: "https://products.example/rpc"},
			}},
		)
		require.ErrorContains(t, err, `subgraph "products-feature" (feature flag "beta")`)
		require.NotContains(t, err.Error(), `subgraph "products"`)
	})

	t.Run("router plugins are outside global protocol selection", func(t *testing.T) {
		err := validateGRPCSubgraphRoutingURLs(grpcTestRouterConfig("plugin", "dns:///plugin:443", true), grpcprotocol.ProtocolConnectRPC, config.OverrideRoutingURLConfiguration{}, config.OverridesConfiguration{})
		require.NoError(t, err)
	})
}

func TestValidateConnectRPCTLSConfiguration(t *testing.T) {
	t.Run("native gRPC accepts gRPC TLS settings", func(t *testing.T) {
		err := validateConnectRPCTLSConfiguration(
			grpcTestRouterConfig("products", "dns:///products:443", false),
			grpcprotocol.ProtocolGRPC,
			config.GRPCClientTLSConfiguration{Subgraphs: map[string]config.GRPCTLSClientCertConfiguration{
				"products": {Enabled: true},
			}},
			zap.NewNop(),
		)
		require.NoError(t, err)
	})

	t.Run("ConnectRPC rejects enabled per-subgraph gRPC TLS", func(t *testing.T) {
		err := validateConnectRPCTLSConfiguration(
			grpcTestRouterConfig("products", "https://products.example/rpc", false),
			grpcprotocol.ProtocolConnectRPC,
			config.GRPCClientTLSConfiguration{Subgraphs: map[string]config.GRPCTLSClientCertConfiguration{
				"products": {Enabled: true},
			}},
			zap.NewNop(),
		)
		require.ErrorContains(t, err, `tls.client_grpc.subgraphs["products"]`)
		require.ErrorContains(t, err, "tls.client.subgraphs")
	})

	t.Run("ConnectRPC warns for global gRPC TLS", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		err := validateConnectRPCTLSConfiguration(
			grpcTestRouterConfig("products", "https://products.example/rpc", false),
			grpcprotocol.ProtocolConnectRPC,
			config.GRPCClientTLSConfiguration{All: config.GRPCTLSClientCertConfiguration{Enabled: true}},
			zap.New(core),
		)
		require.NoError(t, err)
		require.Equal(t, 1, logs.Len())
		require.Contains(t, logs.All()[0].Message, "tls.client_grpc.all")
		require.Equal(t, []interface{}{"products"}, logs.All()[0].ContextMap()["subgraphs"])
	})

	t.Run("ConnectRPC ignores gRPC TLS configured only for plugins", func(t *testing.T) {
		remote := grpcTestRouterConfig("products", "https://products.example/rpc", false)
		plugin := grpcTestRouterConfig("plugin", "dns:///plugin:443", true)
		remote.EngineConfig.DatasourceConfigurations = append(remote.EngineConfig.DatasourceConfigurations, plugin.EngineConfig.DatasourceConfigurations...)
		remote.Subgraphs = append(remote.Subgraphs, plugin.Subgraphs...)

		err := validateConnectRPCTLSConfiguration(
			remote,
			grpcprotocol.ProtocolConnectRPC,
			config.GRPCClientTLSConfiguration{Subgraphs: map[string]config.GRPCTLSClientCertConfiguration{
				"plugin": {Enabled: true},
			}},
			zap.NewNop(),
		)
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
