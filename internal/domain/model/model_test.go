package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewModelProviderNormalizesIdentityAndScope(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	actorID := uuid.New()
	provider, err := NewModelProvider(
		value.ModelScopeWorkspace,
		&workspaceID,
		" OpenAI_Prod ",
		" 生产连接 ",
		" description ",
		" OpenAI ",
		map[string]any{"timeout_seconds": 60},
		[]byte{1, 2, 3},
		actorID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if provider.Name != "openai_prod" {
		t.Fatalf("name = %q, want openai_prod", provider.Name)
	}
	if provider.DisplayName != "生产连接" {
		t.Fatalf("display_name = %q", provider.DisplayName)
	}
	if provider.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", provider.Provider)
	}
	if provider.Status != value.ModelStatusActive {
		t.Fatalf("status = %q, want active", provider.Status)
	}
	if provider.WorkspaceID == nil || *provider.WorkspaceID != workspaceID {
		t.Fatalf("workspace_id = %v, want %s", provider.WorkspaceID, workspaceID)
	}
	if provider.CreatedBy == nil || *provider.CreatedBy != actorID {
		t.Fatalf("created_by = %v, want %s", provider.CreatedBy, actorID)
	}
	if provider.CreatedAt.IsZero() || !provider.CreatedAt.Equal(provider.UpdatedAt) {
		t.Fatalf("timestamps = %v / %v", provider.CreatedAt, provider.UpdatedAt)
	}
}

func TestNewModelProviderRejectsInvalidScopePairAndText(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	actorID := uuid.New()
	tests := []struct {
		name        string
		scope       value.ModelScope
		workspaceID *uuid.UUID
		machineName string
		displayName string
		description string
		provider    string
	}{
		{name: "platform with workspace", scope: value.ModelScopePlatform, workspaceID: &workspaceID, machineName: "provider", provider: "openai"},
		{name: "workspace without workspace", scope: value.ModelScopeWorkspace, machineName: "provider", provider: "openai"},
		{name: "invalid machine name", scope: value.ModelScopePlatform, machineName: "1 bad", provider: "openai"},
		{name: "display too long", scope: value.ModelScopePlatform, machineName: "provider", displayName: strings.Repeat("界", 256), provider: "openai"},
		{name: "description too long", scope: value.ModelScopePlatform, machineName: "provider", description: strings.Repeat("界", 2001), provider: "openai"},
		{name: "provider non ascii", scope: value.ModelScopePlatform, machineName: "provider", provider: "千帆"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewModelProvider(tt.scope, tt.workspaceID, tt.machineName, tt.displayName, tt.description, tt.provider, nil, nil, actorID)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestNewEmbeddingModelNormalizesAndValidatesIdentity(t *testing.T) {
	t.Parallel()

	dimension := 1024
	actorID := uuid.New()
	got, err := NewModel(
		uuid.New(),
		" Embed_V4 ",
		"",
		"",
		value.ModelTypeEmbedding,
		" text-embedding-v4 ",
		&dimension,
		map[string]any{"batch_size": 32},
		actorID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "embed_v4" || got.DisplayName != "embed_v4" {
		t.Fatalf("model = %#v", got)
	}
	if got.ModelName != "text-embedding-v4" {
		t.Fatalf("model_name = %q", got.ModelName)
	}
	if got.Dimensions == nil || *got.Dimensions != 1024 {
		t.Fatalf("dimensions = %v", got.Dimensions)
	}
	if got.Status != value.ModelStatusActive {
		t.Fatalf("status = %q, want active", got.Status)
	}
}

func TestNewModelValidatesTypeDimensionAndRequiredFields(t *testing.T) {
	t.Parallel()

	validDimension := 1024
	unsupportedDimension := 1536
	actorID := uuid.New()
	providerID := uuid.New()
	tests := []struct {
		name       string
		providerID uuid.UUID
		modelType  value.ModelType
		modelName  string
		dimensions *int
	}{
		{name: "missing provider", modelType: value.ModelTypeEmbedding, modelName: "model", dimensions: &validDimension},
		{name: "missing model name", providerID: providerID, modelType: value.ModelTypeEmbedding, dimensions: &validDimension},
		{name: "embedding without dimensions", providerID: providerID, modelType: value.ModelTypeEmbedding, modelName: "model"},
		{name: "unsupported embedding dimensions", providerID: providerID, modelType: value.ModelTypeEmbedding, modelName: "model", dimensions: &unsupportedDimension},
		{name: "llm with dimensions", providerID: providerID, modelType: value.ModelTypeLLM, modelName: "model", dimensions: &validDimension},
		{name: "unknown type", providerID: providerID, modelType: value.ModelType("asr"), modelName: "model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewModel(tt.providerID, "model", "", "", tt.modelType, tt.modelName, tt.dimensions, nil, actorID)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}
