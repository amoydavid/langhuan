package service

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// ResolvedEmbeddingClient is a validated runtime client plus its immutable model facts.
type ResolvedEmbeddingClient struct {
	Client                embeddingport.EmbeddingClient
	ModelID, ProviderID   uuid.UUID
	ModelName             string
	Dimensions, BatchSize int
}

// EmbeddingClientResolver resolves an active Embedding model visible to one Workspace.
type EmbeddingClientResolver interface {
	Resolve(context.Context, uuid.UUID, uuid.UUID) (*ResolvedEmbeddingClient, error)
}

type embeddingClientResolver struct {
	models   ModelRepository
	cipher   embeddingport.CredentialCipher
	registry embeddingport.FactoryRegistry
}

// NewEmbeddingClientResolver creates the shared runtime Embedding resolver.
func NewEmbeddingClientResolver(
	models ModelRepository,
	cipher embeddingport.CredentialCipher,
	registry embeddingport.FactoryRegistry,
) EmbeddingClientResolver {
	return &embeddingClientResolver{models: models, cipher: cipher, registry: registry}
}

func (r *embeddingClientResolver) Resolve(
	ctx context.Context,
	workspaceID, modelID uuid.UUID,
) (*ResolvedEmbeddingClient, error) {
	if workspaceID == uuid.Nil || modelID == uuid.Nil {
		return nil, fmt.Errorf("%w: Workspace/Model ID 不能为空", domainerrors.ErrValidation)
	}
	resolved, err := r.models.GetVisible(ctx, workspaceID, modelID)
	if err != nil {
		return nil, err
	}
	return buildResolvedEmbeddingClient(ctx, resolved, r.cipher, r.registry, true)
}

func buildResolvedEmbeddingClient(
	ctx context.Context,
	resolved *model.ResolvedModel,
	cipher embeddingport.CredentialCipher,
	registry embeddingport.FactoryRegistry,
	requireActive bool,
) (*ResolvedEmbeddingClient, error) {
	if resolved == nil || resolved.Model == nil || resolved.Provider == nil ||
		resolved.Model.ID == uuid.Nil || resolved.Provider.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: Model/Provider 聚合无效", domainerrors.ErrValidation)
	}
	if resolved.Model.Type != value.ModelTypeEmbedding {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	if requireActive {
		if resolved.Provider.Status != value.ModelStatusActive {
			return nil, domainerrors.ErrProviderDisabled
		}
		if resolved.Model.Status != value.ModelStatusActive {
			return nil, domainerrors.ErrModelDisabled
		}
	}
	if resolved.Model.Dimensions == nil || !value.IsSupportedEmbeddingDimension(*resolved.Model.Dimensions) {
		return nil, domainerrors.ErrUnsupportedEmbeddingDimension
	}
	batchSize, err := embeddingBatchSize(resolved.Model.Parameters)
	if err != nil {
		return nil, err
	}
	plaintext, err := cipher.Decrypt(resolved.Provider.ID, resolved.Provider.CredentialsCiphertext)
	if err != nil {
		return nil, fmt.Errorf("Provider %s: %w", resolved.Provider.ID, domainerrors.ErrCredentialDecryption)
	}
	defer clearBytes(plaintext)
	factory, err := registry.Factory(resolved.Model.Type, resolved.Provider.Provider)
	if err != nil {
		return nil, err
	}
	dimensions := *resolved.Model.Dimensions
	client, err := factory.NewClient(ctx, embeddingport.ClientInput{
		ProviderID: resolved.Provider.ID, Scope: resolved.Provider.Scope,
		Config: resolved.Provider.Config, CredentialsJSON: plaintext,
		ModelName: resolved.Model.ModelName, Dimensions: dimensions, Parameters: resolved.Model.Parameters,
	})
	if err != nil {
		return nil, err
	}
	if client == nil || client.Dimension() != dimensions {
		return nil, fmt.Errorf("%w: configured=%d client=%d", domainerrors.ErrDimensionMismatch, dimensions, clientDimension(client))
	}
	return &ResolvedEmbeddingClient{
		Client: client, ModelID: resolved.Model.ID, ProviderID: resolved.Provider.ID,
		ModelName: resolved.Model.ModelName, Dimensions: dimensions, BatchSize: batchSize,
	}, nil
}

func embeddingBatchSize(parameters map[string]any) (int, error) {
	raw, ok := parameters["batch_size"]
	if !ok {
		return 0, fmt.Errorf("%w: batch_size 缺失", domainerrors.ErrInvalidProviderConfig)
	}
	var batchSize int
	switch value := raw.(type) {
	case int:
		batchSize = value
	case int64:
		batchSize = int(value)
	case float64:
		if math.Trunc(value) != value {
			return 0, fmt.Errorf("%w: batch_size 必须是整数", domainerrors.ErrInvalidProviderConfig)
		}
		batchSize = int(value)
	default:
		return 0, fmt.Errorf("%w: batch_size 类型无效", domainerrors.ErrInvalidProviderConfig)
	}
	if batchSize < 1 || batchSize > 200 {
		return 0, fmt.Errorf("%w: batch_size 必须在 1 到 200 之间", domainerrors.ErrInvalidProviderConfig)
	}
	return batchSize, nil
}

func clientDimension(client embeddingport.EmbeddingClient) int {
	if client == nil {
		return 0
	}
	return client.Dimension()
}
