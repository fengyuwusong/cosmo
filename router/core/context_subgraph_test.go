package core

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	rcontext "github.com/wundergraph/cosmo/router/internal/context"
)

func TestActiveSubgraphPrefersExplicitIdentity(t *testing.T) {
	resolver := NewSubgraphResolver([]Subgraph{
		{Id: "products-id", Name: "products", Url: mustParseURL(t, "https://products.example/rpc"), UrlString: "https://products.example/rpc"},
		{Id: "other-id", Name: "other", Url: mustParseURL(t, "https://other.example/graphql"), UrlString: "https://other.example/graphql"},
	})
	requestContext := &requestContext{subgraphResolver: resolver}

	request, err := http.NewRequestWithContext(
		context.WithValue(context.Background(), rcontext.CurrentSubgraphContextKey{}, "products"),
		http.MethodPost,
		"https://products.example/rpc/package.Service/Method",
		http.NoBody,
	)
	require.NoError(t, err)

	require.Equal(t, "products-id", requestContext.ActiveSubgraph(request).Id)
}

func TestActiveSubgraphResolvesExplicitIdentityWithoutURL(t *testing.T) {
	resolver := NewSubgraphResolver([]Subgraph{
		{Id: "products-id", Name: "products"},
	})
	requestContext := &requestContext{subgraphResolver: resolver}
	request := &http.Request{
		Method: http.MethodPost,
		Header: make(http.Header),
	}
	request = request.WithContext(context.WithValue(request.Context(), rcontext.CurrentSubgraphContextKey{}, "products"))

	require.Equal(t, "products-id", requestContext.ActiveSubgraph(request).Id)
}

func TestActiveSubgraphRetainsExactURLFallback(t *testing.T) {
	resolver := NewSubgraphResolver([]Subgraph{
		{Id: "products-id", Name: "products", Url: mustParseURL(t, "https://products.example/graphql"), UrlString: "https://products.example/graphql"},
	})
	requestContext := &requestContext{subgraphResolver: resolver}
	request, err := http.NewRequest(http.MethodPost, "https://products.example/graphql", http.NoBody)
	require.NoError(t, err)

	require.Equal(t, "products-id", requestContext.ActiveSubgraph(request).Id)
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return parsed
}
