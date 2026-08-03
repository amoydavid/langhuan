package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// ConnectionTestText is the only text sent by model connection tests.
const ConnectionTestText = "langhuan embedding connection test"

// ModelConnectionTestService performs non-persistent live Embedding checks.
type ModelConnectionTestService struct {
	models   ModelRepository
	cipher   embeddingport.CredentialCipher
	registry embeddingport.FactoryRegistry
}

// NewModelConnectionTestService creates a connection-test service.
func NewModelConnectionTestService(models ModelRepository, cipher embeddingport.CredentialCipher, registry embeddingport.FactoryRegistry) *ModelConnectionTestService {
	return &ModelConnectionTestService{models: models, cipher: cipher, registry: registry}
}

// TestWorkspace tests a model visible to one Workspace, including disabled records.
func (s *ModelConnectionTestService) TestWorkspace(ctx context.Context, workspaceID, modelID uuid.UUID) (*dto.ConnectionTestResult, error) {
	resolved, err := s.models.GetVisible(ctx, workspaceID, modelID)
	if err != nil {
		return nil, err
	}
	return s.test(ctx, resolved)
}

// TestPlatform tests one platform model, including disabled records.
func (s *ModelConnectionTestService) TestPlatform(ctx context.Context, modelID uuid.UUID) (*dto.ConnectionTestResult, error) {
	resolved, err := s.models.GetPlatform(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return s.test(ctx, resolved)
}

func (s *ModelConnectionTestService) test(ctx context.Context, resolved *model.ResolvedModel) (*dto.ConnectionTestResult, error) {
	runtimeClient, err := buildResolvedEmbeddingClient(ctx, resolved, s.cipher, s.registry, false)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	result, err := runtimeClient.Client.Embed(ctx, embeddingport.EmbedInput{Texts: []string{ConnectionTestText}})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Vectors) != 1 || len(result.Vectors[0]) == 0 {
		return nil, domainerrors.ErrInvalidEmbeddingResponse
	}
	if len(result.Vectors[0]) != runtimeClient.Dimensions {
		return nil, domainerrors.ErrDimensionMismatch
	}
	return &dto.ConnectionTestResult{OK: true, Dimensions: len(result.Vectors[0]), DurationMS: time.Since(started).Milliseconds()}, nil
}
