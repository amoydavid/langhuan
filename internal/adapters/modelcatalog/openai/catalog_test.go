package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

func TestCatalogListsAndFiltersOpenAICompatibleModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" || request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request = %s %s authorization=%q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "BAAI/bge-m3", "owned_by": "siliconflow"},
				{"id": "Qwen/Qwen3-Embedding-8B", "owned_by": "siliconflow"},
			},
		})
	}))
	t.Cleanup(server.Close)

	typeEmbedding := value.ModelTypeEmbedding
	items, err := NewCatalog().ListModels(context.Background(), modelcatalogport.Input{
		Scope: value.ModelScopePlatform,
		Config: map[string]any{
			"base_url": server.URL + "/v1", "timeout_seconds": 5,
		},
		CredentialsJSON: json.RawMessage(`{"api_key":"secret"}`),
		ModelType:       &typeEmbedding,
		Query:           "bge",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "BAAI/bge-m3" || items[0].Type == nil || *items[0].Type != value.ModelTypeEmbedding {
		t.Fatalf("catalog items = %#v", items)
	}
}
