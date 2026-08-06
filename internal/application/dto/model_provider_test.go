package dto

import (
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestProviderDTOIncludesCapabilitiesAndCounts(t *testing.T) {
	t.Parallel()
	provider := &model.ModelProvider{
		ID: uuid.New(), Scope: value.ModelScopePlatform, Name: "siliconflow", DisplayName: "SiliconFlow",
		Provider: "siliconflow", Config: map[string]any{}, Status: value.ModelStatusActive,
	}
	counts := ProviderModelCounts{Total: 5, Active: 4, Embedding: 3, Rerank: 2}
	got := ModelProviderFromModel(provider, []string{"api_key"}, []value.ProviderCapability{
		value.CapabilityEmbedding, value.CapabilityRerank,
	}, true, counts)
	if got.ModelCounts != counts || len(got.Capabilities) != 2 || !got.ModelCatalog {
		t.Fatalf("provider DTO = %#v", got)
	}
}
