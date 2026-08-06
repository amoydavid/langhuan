package siliconflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

func TestSiliconFlowModelCatalogListsAndNormalizesModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer shared-secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"BAAI/bge-m3","description":"multilingual","owned_by":"siliconflow"},{"id":"BAAI/bge-reranker-v2-m3"}]}`)
	}))
	t.Cleanup(server.Close)
	catalog := &modelCatalog{}
	items, err := catalog.ListModels(context.Background(), modelcatalogport.Input{
		Scope:           value.ModelScopePlatform,
		Config:          map[string]any{"base_url": server.URL, "timeout_seconds": 30},
		CredentialsJSON: []byte(`{"api_key":"shared-secret"}`),
		ModelType:       ptrCatalogType(value.ModelTypeEmbedding),
		Query:           "BGE-M3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "BAAI/bge-m3" || items[0].Dimensions == nil || *items[0].Dimensions != 1024 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Parameters["batch_size"] != 32 || !items[0].Available {
		t.Fatalf("normalized metadata = %#v", items[0])
	}
}

func TestSiliconFlowModelCatalogMapsUpstreamFailureSafely(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret upstream details", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	_, err := (&modelCatalog{}).ListModels(context.Background(), modelcatalogport.Input{
		Scope:           value.ModelScopePlatform,
		Config:          map[string]any{"base_url": server.URL, "timeout_seconds": 30},
		CredentialsJSON: []byte(`{"api_key":"secret"}`),
	})
	if err == nil || !errors.Is(err, domainerrors.ErrCatalogUnavailable) || strings.Contains(err.Error(), "secret upstream") {
		t.Fatalf("error = %v", err)
	}
}

func ptrCatalogType(modelType value.ModelType) *value.ModelType { return &modelType }
