package grpcprotocol

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/wundergraph/cosmo/router/pkg/config"
	grpcdatasource "github.com/wundergraph/graphql-go-tools/v2/pkg/engine/datasource/grpc_datasource"
)

// BuildConnectTransports creates a map of subgraphName → RPCTransport
// for all subgraphs configured to use ConnectRPC.
// Returns nil if no subgraphs are configured for Connect, or an error if
// any ConnectRPC subgraph declares a routing URL whose scheme is not
// reachable over TCP/HTTP (for example `unix:` or `vsock:`).
func BuildConnectTransports(
	cfg *config.GRPCProtocolConfiguration,
	grpcSubgraphURLs map[string]string,
	subgraphHTTPClients map[string]*http.Client,
	defaultHTTPClient *http.Client,
) (map[string]grpcdatasource.RPCTransport, error) {
	if cfg == nil {
		return nil, nil
	}

	transports := make(map[string]grpcdatasource.RPCTransport)

	for subgraphName, routingURL := range grpcSubgraphURLs {
		if ResolveProtocol(cfg, subgraphName) != ProtocolConnectRPC {
			continue
		}

		baseURL, err := normalizeConnectBaseURL(routingURL)
		if err != nil {
			return nil, fmt.Errorf("subgraph %q: %w", subgraphName, err)
		}

		httpClient := defaultHTTPClient
		if sgClient, ok := subgraphHTTPClients[subgraphName]; ok {
			httpClient = sgClient
		}

		var connectEncoding grpcdatasource.ConnectEncoding
		if ResolveEncoding(cfg, subgraphName) == EncodingJSON {
			connectEncoding = grpcdatasource.ConnectEncodingJSON
		} else {
			connectEncoding = grpcdatasource.ConnectEncodingProtobuf
		}

		transports[subgraphName] = grpcdatasource.NewConnectTransport(
			grpcdatasource.ConnectTransportConfig{
				BaseURL:    baseURL,
				HTTPClient: httpClient,
				Encoding:   connectEncoding,
			},
		)
	}

	if len(transports) == 0 {
		return nil, nil
	}
	return transports, nil
}

// TCP-compatible name-resolver schemes that can be rewritten into an
// HTTP URL the ConnectRPC client can dial. The remaining gRPC schemes
// (`unix:`, `unix-abstract:`, `vsock:`) describe transports the
// standard *http.Client cannot dial, so they are rejected.
var tcpCompatibleGRPCSchemes = []string{"passthrough:", "dns:", "ipv4:", "ipv6:"}

// nonTCPGRPCSchemes are the gRPC name-resolver schemes that point at
// transports incompatible with HTTP-over-TCP. Listed longest-prefix-first
// so detection works without false positives.
var nonTCPGRPCSchemes = []string{"unix-abstract:", "unix:", "vsock:"}

// normalizeConnectBaseURL converts a routing URL declared in the federated
// graph — which may use the gRPC name resolver conventions like
// "dns:///host:port" or "dns:host:port", or a bare host:port — into the
// http URL that the ConnectRPC HTTP client expects. URLs that already use
// http or https are returned as-is.
//
// Schemes that point at non-TCP transports (`unix:`, `unix-abstract:`,
// `vsock:`) are rejected with an error: the ConnectRPC transport runs on
// top of the standard *http.Client, which only knows how to dial TCP, so
// silently rewriting `unix:///tmp/x.sock` into `http://tmp/x.sock` would
// hand callers a transport that fails at first request. If we ever grow
// a custom dialer for these schemes, this is the natural place to drop
// the rejection.
func normalizeConnectBaseURL(routingURL string) (string, error) {
	if routingURL == "" {
		return routingURL, nil
	}
	if strings.HasPrefix(routingURL, "http://") || strings.HasPrefix(routingURL, "https://") {
		return routingURL, nil
	}

	for _, prefix := range nonTCPGRPCSchemes {
		if strings.HasPrefix(routingURL, prefix) {
			scheme := strings.TrimSuffix(prefix, ":")
			return "", fmt.Errorf("connectrpc transport cannot dial %q endpoints (routing URL %q); use http(s):// instead, or keep this subgraph on the native gRPC protocol", scheme, routingURL)
		}
	}

	for _, prefix := range tcpCompatibleGRPCSchemes {
		if !strings.HasPrefix(routingURL, prefix) {
			continue
		}
		rest := routingURL[len(prefix):]
		// `scheme://authority/endpoint` and `scheme:///endpoint` both end up
		// with a leading `//`; strip the authority and the path separator so
		// only the endpoint survives.
		if strings.HasPrefix(rest, "//") {
			after := rest[2:]
			if i := strings.Index(after, "/"); i >= 0 {
				rest = after[i+1:]
			} else {
				rest = after
			}
		}
		return "http://" + rest, nil
	}
	return "http://" + routingURL, nil
}
