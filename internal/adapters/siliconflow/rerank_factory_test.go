package siliconflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

func TestSiliconFlowRerankUsesSharedCredentialAndEndpoint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/rerank" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer shared-secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"results":[{"index":0,"relevance_score":0.9},{"index":1,"relevance_score":0.2}]}`))
	}))
	t.Cleanup(server.Close)

	factory := NewRerankFactory()
	config, credentials, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopePlatform,
		Config:      json.RawMessage(fmt.Sprintf(`{"base_url":%q}`, server.URL)),
		Credentials: json.RawMessage(`{"api_key":"shared-secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(rerankport.ModelDecodeInput{
		ModelName: "BAAI/bge-reranker-v2-m3", Parameters: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), rerankport.ClientInput{
		ProviderID: uuid.New(), Scope: value.ModelScopePlatform, Config: config,
		CredentialsJSON: credentials, ModelName: "BAAI/bge-reranker-v2-m3", Parameters: parameters,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query: "知识检索", Documents: []rerankport.Document{{ID: "a", Text: "相关"}, {ID: "b", Text: "无关"}}, TopN: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].DocumentID != "a" {
		t.Fatalf("result = %#v", result)
	}
}
