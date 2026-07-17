package service

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetadataFromConnectHeadersDecodesBinaryValues(t *testing.T) {
	headers := http.Header{
		"X-Tenant-Id": []string{"acme"},
		"X-Token-Bin": []string{base64.StdEncoding.EncodeToString([]byte("raw-binary"))},
	}

	result, err := metadataFromConnectHeaders(headers)
	require.NoError(t, err)
	require.Equal(t, []string{"acme"}, result.Get("x-tenant-id"))
	require.Equal(t, []string{"raw-binary"}, result.Get("x-token-bin"))
}
