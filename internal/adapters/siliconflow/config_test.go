package siliconflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDecodeProviderNormalizesSiliconFlowOnce(t *testing.T) {
	t.Parallel()
	config, credentials, err := DecodeProvider(
		value.ModelScopePlatform,
		json.RawMessage(`{"base_url":"https://api.siliconflow.cn","timeout_seconds":60,"retry_times":2}`),
		json.RawMessage(`{"api_key":"secret"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if config["embedding_endpoint_path"] != "/v1/embeddings" || config["rerank_endpoint_path"] != "/v1/rerank" {
		t.Fatalf("config = %#v", config)
	}
	if !bytes.Contains(credentials, []byte(`"api_key"`)) || bytes.Contains(credentials, []byte(`custom_headers`)) {
		t.Fatalf("credentials = %s", credentials)
	}
}

func TestDecodeProviderNormalizesLegacyVersionedBaseURL(t *testing.T) {
	t.Parallel()
	config, _, err := DecodeProvider(
		value.ModelScopePlatform,
		json.RawMessage(`{"base_url":"https://api.siliconflow.cn/v1"}`),
		json.RawMessage(`{"api_key":"secret"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if config["base_url"] != "https://api.siliconflow.cn" {
		t.Fatalf("base_url = %#v, want origin without duplicated /v1 prefix", config["base_url"])
	}
}

func TestDecodeProviderRejectsUnknownAndUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		scope       value.ModelScope
		config      string
		credentials string
		want        error
	}{
		{name: "unknown field", scope: value.ModelScopePlatform, config: `{"extra":true}`, credentials: `{"api_key":"secret"}`, want: domainerrors.ErrInvalidProviderConfig},
		{name: "unknown credential", scope: value.ModelScopePlatform, config: `{}`, credentials: `{"api_key":"secret","custom_headers":{}}`, want: domainerrors.ErrInvalidProviderConfig},
		{name: "missing key", scope: value.ModelScopePlatform, config: `{}`, credentials: `{}`, want: domainerrors.ErrCredentialsRequired},
		{name: "workspace private host", scope: value.ModelScopeWorkspace, config: `{"base_url":"http://127.0.0.1:9000"}`, credentials: `{"api_key":"secret"}`, want: domainerrors.ErrInvalidProviderConfig},
		{name: "unsafe embedding path", scope: value.ModelScopePlatform, config: `{"embedding_endpoint_path":"https://evil.example/embeddings"}`, credentials: `{"api_key":"secret"}`, want: domainerrors.ErrInvalidProviderConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeProvider(tt.scope, json.RawMessage(tt.config), json.RawMessage(tt.credentials))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSiliconFlowFactoriesShareProviderKey(t *testing.T) {
	t.Parallel()
	if NewEmbeddingFactory().Provider() != ProviderKey || NewRerankFactory().Provider() != ProviderKey {
		t.Fatalf("providers = %q, %q", NewEmbeddingFactory().Provider(), NewRerankFactory().Provider())
	}
}
