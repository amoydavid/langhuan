package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	siliconflowadapter "github.com/dajee/langhuan/internal/adapters/siliconflow"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// TestSiliconFlowRuntimeSupportsEmbeddingAndRerankModels verifies the runtime
// registry contract with one fake connection and one shared Bearer credential.
func TestSiliconFlowRuntimeSupportsEmbeddingAndRerankModels(t *testing.T) {
	var embeddingCalls, rerankCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer shared-secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/embeddings":
			embeddingCalls++
			values := strings.TrimSuffix(strings.Repeat("0,", 1024), ",")
			_, _ = fmt.Fprintf(w, `{"data":[{"embedding":[%s],"index":0}]}`, values)
		case "/v1/rerank":
			rerankCalls++
			_, _ = w.Write([]byte(`{"results":[{"index":0,"relevance_score":0.9},{"index":1,"relevance_score":0.2}]}`))
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)

	embeddingFactory := siliconflowadapter.NewEmbeddingFactory()
	rerankFactory := siliconflowadapter.NewRerankFactory()
	configRaw := json.RawMessage(fmt.Sprintf(`{"base_url":%q}`, server.URL))
	credentialsRaw := json.RawMessage(`{"api_key":"shared-secret"}`)
	config, credentials, err := siliconflowadapter.DecodeProvider(value.ModelScopePlatform, configRaw, credentialsRaw)
	if err != nil {
		t.Fatal(err)
	}
	embeddingParameters, err := embeddingFactory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "BAAI/bge-m3", Dimensions: 1024, Parameters: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	embeddingClient, err := embeddingFactory.NewClient(context.Background(), embeddingport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopePlatform, Config: config, CredentialsJSON: credentials, ModelName: "BAAI/bge-m3", Dimensions: 1024, Parameters: embeddingParameters})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := embeddingClient.Embed(context.Background(), embeddingport.EmbedInput{Texts: []string{"琅嬛"}}); err != nil {
		t.Fatal(err)
	}
	rerankParameters, err := rerankFactory.DecodeModel(rerankport.ModelDecodeInput{ModelName: "BAAI/bge-reranker-v2-m3", Parameters: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	rerankClient, err := rerankFactory.NewClient(context.Background(), rerankport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopePlatform, Config: config, CredentialsJSON: credentials, ModelName: "BAAI/bge-reranker-v2-m3", Parameters: rerankParameters})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rerankClient.Rerank(context.Background(), rerankport.RerankInput{Query: "知识", Documents: []rerankport.Document{{ID: "a", Text: "相关"}, {ID: "b", Text: "无关"}}, TopN: 2}); err != nil {
		t.Fatal(err)
	}
	if embeddingCalls != 1 || rerankCalls != 1 {
		t.Fatalf("calls = embedding:%d rerank:%d", embeddingCalls, rerankCalls)
	}
}
