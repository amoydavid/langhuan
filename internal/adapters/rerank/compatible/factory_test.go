package compatible_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/adapters/rerank/compatible"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

func TestFactoryProviderAndCredentialFields(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	if factory.Provider() != "rerank_compatible" {
		t.Fatalf("provider = %q", factory.Provider())
	}
	if fields := factory.CredentialFields(); len(fields) != 2 {
		t.Fatalf("credential fields = %#v", fields)
	}
}

func TestFactorySupportsExplicitProviderKey(t *testing.T) {
	t.Parallel()
	if got := compatible.NewFactoryWithProvider("siliconflow").Provider(); got != "siliconflow" {
		t.Fatalf("provider = %q", got)
	}
}

func TestFactoryDecodesProviderAndModel(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	config, credentials, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace,
		Config: json.RawMessage(`{
			"base_url":"https://rerank.example.com",
			"endpoint_path":"/v1/rerank",
			"timeout_seconds":30,
			"retry_times":2
		}`),
		Credentials: json.RawMessage(`{"api_key":"secret","custom_headers":{"X-Tenant":"acme"}}`),
	})
	if err != nil {
		t.Fatalf("decode provider err = %v", err)
	}
	if config["endpoint_path"] != "/v1/rerank" {
		t.Fatalf("endpoint_path = %#v", config["endpoint_path"])
	}
	if len(credentials) == 0 {
		t.Fatal("credentials empty")
	}

	parameters, err := factory.DecodeModel(rerankport.ModelDecodeInput{
		ModelName:  "BAAI/bge-reranker-v2-m3",
		Parameters: json.RawMessage(`{"max_documents":100,"max_query_chars":4096,"max_document_chars":8192}`),
	})
	if err != nil {
		t.Fatalf("decode model err = %v", err)
	}
	if parameters["max_documents"] != float64(100) {
		t.Fatalf("max_documents = %#v", parameters["max_documents"])
	}
}

func TestFactoryRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	_, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopeWorkspace,
		Config:      json.RawMessage(`{"base_url":"https://rerank.example.com","extra":"x"}`),
		Credentials: json.RawMessage(`{"api_key":"secret"}`),
	})
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("unknown config err = %v", err)
	}
	_, err = factory.DecodeModel(rerankport.ModelDecodeInput{
		ModelName:  "m",
		Parameters: json.RawMessage(`{"max_documents":100,"max_query_chars":4096,"max_document_chars":8192,"extra":1}`),
	})
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("unknown param err = %v", err)
	}
}

func TestFactoryAppliesDefaults(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	config, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopeWorkspace,
		Config:      json.RawMessage(`{"base_url":"https://rerank.example.com"}`),
		Credentials: json.RawMessage(`{"api_key":"secret"}`),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if config["endpoint_path"] != "/v1/rerank" {
		t.Fatalf("default endpoint = %#v", config["endpoint_path"])
	}
	if config["timeout_seconds"] != float64(30) {
		t.Fatalf("default timeout = %#v", config["timeout_seconds"])
	}
	if config["retry_times"] != float64(2) {
		t.Fatalf("default retry = %#v", config["retry_times"])
	}

	parameters, err := factory.DecodeModel(rerankport.ModelDecodeInput{
		ModelName:  "m",
		Parameters: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if parameters["max_documents"] != float64(100) {
		t.Fatalf("default max_documents = %#v", parameters["max_documents"])
	}
}

func TestFactoryRejectsBadProviderConfig(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()

	tests := []struct {
		name   string
		config string
	}{
		{"workspace missing base_url", `{"base_url":""}`},
		{"base_url with query", `{"base_url":"https://rerank.example.com?x=1"}`},
		{"base_url with fragment", `{"base_url":"https://rerank.example.com#sec"}`},
		{"base_url with userinfo", `{"base_url":"https://user:pass@rerank.example.com"}`},
		{"endpoint absolute", `{"base_url":"https://rerank.example.com","endpoint_path":"https://evil.com/x"}`},
		{"endpoint with host", `{"base_url":"https://rerank.example.com","endpoint_path":"//evil.com/x"}`},
		{"endpoint with query", `{"base_url":"https://rerank.example.com","endpoint_path":"/v1/rerank?x=1"}`},
		{"endpoint with dotdot", `{"base_url":"https://rerank.example.com","endpoint_path":"/v1/../rerank"}`},
		{"endpoint no slash", `{"base_url":"https://rerank.example.com","endpoint_path":"v1/rerank"}`},
		{"timeout too small", `{"base_url":"https://rerank.example.com","timeout_seconds":0}`},
		{"timeout too large", `{"base_url":"https://rerank.example.com","timeout_seconds":121}`},
		{"retry negative", `{"base_url":"https://rerank.example.com","retry_times":-1}`},
		{"retry too large", `{"base_url":"https://rerank.example.com","retry_times":4}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
				Scope:       value.ModelScopeWorkspace,
				Config:      json.RawMessage(tt.config),
				Credentials: json.RawMessage(`{"api_key":"secret"}`),
			})
			if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFactoryRejectsWorkspacePrivateEndpoint(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	_, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopeWorkspace,
		Config:      json.RawMessage(`{"base_url":"https://127.0.0.1:8443"}`),
		Credentials: json.RawMessage(`{"api_key":"secret"}`),
	})
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestFactoryRejectsMissingApiKey(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	_, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopeWorkspace,
		Config:      json.RawMessage(`{"base_url":"https://rerank.example.com"}`),
		Credentials: json.RawMessage(`{}`),
	})
	if !errors.Is(err, domainerrors.ErrCredentialsRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestFactoryRejectsReservedHeaders(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	for _, header := range []string{"Authorization", "Host", "Content-Type", "Accept-Encoding"} {
		creds, _ := json.Marshal(map[string]any{
			"api_key":        "secret",
			"custom_headers": map[string]string{header: "x"},
		})
		_, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
			Scope:       value.ModelScopeWorkspace,
			Config:      json.RawMessage(`{"base_url":"https://rerank.example.com"}`),
			Credentials: creds,
		})
		if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
			t.Fatalf("header %q err = %v", header, err)
		}
	}
}

func TestFactoryRejectsCRLFHeaders(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	_, _, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopeWorkspace,
		Config:      json.RawMessage(`{"base_url":"https://rerank.example.com"}`),
		Credentials: json.RawMessage(`{"api_key":"secret","custom_headers":{"X-Tenant":"acme\r\nEvil: yes"}}`),
	})
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestFactoryRejectsBadModelParameters(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	tests := []struct {
		name       string
		modelName  string
		parameters string
	}{
		{"empty model name", "", `{}`},
		{"max_documents too small", "m", `{"max_documents":49}`},
		{"max_documents too large", "m", `{"max_documents":201}`},
		{"max_query_chars too small", "m", `{"max_query_chars":255}`},
		{"max_query_chars too large", "m", `{"max_query_chars":4097}`},
		{"max_document_chars too small", "m", `{"max_document_chars":511}`},
		{"max_document_chars too large", "m", `{"max_document_chars":32769}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := factory.DecodeModel(rerankport.ModelDecodeInput{
				ModelName:  tt.modelName,
				Parameters: json.RawMessage(tt.parameters),
			})
			if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFactoryNewClient(t *testing.T) {
	t.Parallel()
	factory := compatible.NewFactory()
	config, credentialsJSON, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopeWorkspace,
		Config:      json.RawMessage(`{"base_url":"https://rerank.example.com"}`),
		Credentials: json.RawMessage(`{"api_key":"secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(rerankport.ModelDecodeInput{
		ModelName:  "bge-reranker-v2-m3",
		Parameters: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := factory.NewClient(t.Context(), rerankport.ClientInput{
		ProviderID:      uuid.New(),
		Scope:           value.ModelScopeWorkspace,
		Config:          config,
		CredentialsJSON: credentialsJSON,
		ModelName:       "bge-reranker-v2-m3",
		Parameters:      parameters,
	})
	if err != nil || c == nil {
		t.Fatalf("client = %#v err = %v", c, err)
	}
}
