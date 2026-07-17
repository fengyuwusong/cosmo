package grpcprotocol

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/wundergraph/cosmo/router/pkg/config"
	grpcdatasource "github.com/wundergraph/graphql-go-tools/v2/pkg/engine/datasource/grpc_datasource"
)

type Protocol string

const (
	ProtocolGRPC       Protocol = "grpc"
	ProtocolConnectRPC Protocol = "connectrpc"
)

type ResolvedConfiguration struct {
	Protocol Protocol
	Encoding grpcdatasource.ConnectEncoding
}

func Resolve(cfg config.GRPCProtocolConfiguration) (ResolvedConfiguration, error) {
	protocol := Protocol(cfg.DefaultProtocol)
	if protocol == "" {
		protocol = ProtocolGRPC
	}

	if protocol != ProtocolGRPC && protocol != ProtocolConnectRPC {
		return ResolvedConfiguration{}, fmt.Errorf("unsupported gRPC subgraph protocol %q; expected grpc or connectrpc", cfg.DefaultProtocol)
	}

	encoding := grpcdatasource.ConnectEncoding(cfg.ConnectRPCEncoding)
	if encoding == "" {
		encoding = grpcdatasource.ConnectEncodingProtobuf
	}

	if encoding != grpcdatasource.ConnectEncodingProtobuf && encoding != grpcdatasource.ConnectEncodingJSON {
		return ResolvedConfiguration{}, fmt.Errorf("unsupported ConnectRPC encoding %q; expected proto or json", cfg.ConnectRPCEncoding)
	}

	return ResolvedConfiguration{Protocol: protocol, Encoding: encoding}, nil
}

func ValidateRoutingURL(protocol Protocol, rawURL string) error {
	if protocol == ProtocolConnectRPC {
		return validateConnectURL(rawURL)
	}

	scheme, _, _ := strings.Cut(rawURL, ":")
	if strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https") {
		return fmt.Errorf("native gRPC requires a gRPC resolver address, not an HTTP(S) URL")
	}

	return nil
}

func validateConnectURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("ConnectRPC requires an absolute http:// or https:// URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("ConnectRPC requires an absolute http:// or https:// URL")
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return fmt.Errorf("ConnectRPC requires an absolute URL with a non-empty host")
	}
	if parsed.User != nil {
		return fmt.Errorf("ConnectRPC routing URLs must not contain user information")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("ConnectRPC routing URLs must not contain query parameters")
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("ConnectRPC routing URLs must not contain fragments")
	}

	return nil
}
