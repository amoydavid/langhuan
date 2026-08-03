package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicURLBuilderPreservesDeploymentPrefix(t *testing.T) {
	builder, err := NewPublicURLBuilder("https://example.com/langhuan")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/langhuan", builder.BaseURL())
	urls := builder.URLs()
	require.Equal(t, "https://example.com/langhuan", urls.BaseURL)
	require.Equal(t, "https://example.com/langhuan/", urls.WebURL)
	require.Equal(t, "https://example.com/langhuan/api/v1", urls.RESTBaseURL)
	require.Equal(t, "https://example.com/langhuan/mcp", urls.MCPURL)
	require.Equal(t, "https://example.com/langhuan/invitations/accept?token=x", builder.Resolve("/invitations/accept?token=x"))
}

func TestPublicURLBuilderTrimsTrailingSlashes(t *testing.T) {
	builder, err := NewPublicURLBuilder("https://example.com/langhuan///")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/langhuan", builder.BaseURL())
	require.Equal(t, "https://example.com/langhuan/api/v1", builder.URLs().RESTBaseURL)
}

func TestPublicURLBuilderRejectsEmptyBaseURL(t *testing.T) {
	_, err := NewPublicURLBuilder("")
	require.ErrorContains(t, err, "server.base_url 不能为空")
	_, err = NewPublicURLBuilder("   ")
	require.ErrorContains(t, err, "server.base_url 不能为空")
}

func TestPublicURLValidationRejectsUnsafeShapesAndProductionHTTP(t *testing.T) {
	for _, raw := range []string{
		"https://user@example.com",
		"https://example.com?q=1",
		"https://example.com/#fragment",
		"ftp://example.com",
		"/relative/path",
		"https:///missing-host",
	} {
		_, err := NewPublicURLBuilder(raw)
		require.Error(t, err, raw)
	}
	local, err := NewPublicURLBuilder("http://127.0.0.1:8080")
	require.NoError(t, err)
	require.ErrorContains(t, local.ValidateProduction(), "HTTPS")
	prod, err := NewPublicURLBuilder("https://langhuan.example.com")
	require.NoError(t, err)
	require.NoError(t, prod.ValidateProduction())
}

func TestPublicURLBuilderDerivesAllThreeEndpoints(t *testing.T) {
	builder, err := NewPublicURLBuilder("http://127.0.0.1:8080")
	require.NoError(t, err)
	urls := builder.URLs()
	require.Equal(t, "http://127.0.0.1:8080/", urls.WebURL)
	require.Equal(t, "http://127.0.0.1:8080/api/v1", urls.RESTBaseURL)
	require.Equal(t, "http://127.0.0.1:8080/mcp", urls.MCPURL)
}
