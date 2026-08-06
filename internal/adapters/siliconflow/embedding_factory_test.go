package siliconflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

func TestSiliconFlowEmbeddingUsesSharedCredentialAndEndpoint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer shared-secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		values := strings.TrimSuffix(strings.Repeat("0,", 1024), ",")
		_, _ = fmt.Fprintf(w, `{"data":[{"embedding":[%s],"index":0}],"model":"BAAI/bge-m3","usage":{"prompt_tokens":1,"total_tokens":1}}`, values)
	}))
	t.Cleanup(server.Close)

	factory := NewEmbeddingFactory()
	config, credentials, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope:       value.ModelScopePlatform,
		Config:      json.RawMessage(fmt.Sprintf(`{"base_url":%q}`, server.URL+"/v1")),
		Credentials: json.RawMessage(`{"api_key":"shared-secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{
		ModelName: "BAAI/bge-m3", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":32}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), embeddingport.ClientInput{
		ProviderID: uuid.New(), Scope: value.ModelScopePlatform, Config: config,
		CredentialsJSON: credentials, ModelName: "BAAI/bge-m3", Dimensions: 1024, Parameters: parameters,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Embed(context.Background(), embeddingport.EmbedInput{Texts: []string{"琅嬛"}}); err != nil {
		t.Fatal(err)
	}
}
