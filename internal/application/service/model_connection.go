package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// ConnectionTestText 是 Embedding 连接测试发送的唯一文本。
const ConnectionTestText = "langhuan embedding connection test"

// RerankConnectionTestQuery 与 RerankConnectionTestDocuments 是 Rerank 连接测试的固定输入。
// 测试只验证网络、协议、数量、唯一索引和有限 score，不要求某个文档必须排第一。
const (
	RerankConnectionTestQuery = "langhuan rerank connection test"
)

// RerankConnectionTestDocuments 返回 Rerank 连接测试使用的固定文档。
func RerankConnectionTestDocuments() []rerankport.Document {
	return []rerankport.Document{
		{ID: "rerank-test-unrelated", Text: "unrelated sample"},
		{ID: "rerank-test-match", Text: RerankConnectionTestQuery},
	}
}

// ModelConnectionTestService performs non-persistent live Embedding/Rerank checks.
type ModelConnectionTestService struct {
	models           ModelRepository
	cipher           embeddingport.CredentialCipher
	embeddingFactory embeddingport.FactoryRegistry
	rerankFactory    rerankport.FactoryRegistry
}

// NewModelConnectionTestService creates a connection-test service.
func NewModelConnectionTestService(
	models ModelRepository,
	cipher embeddingport.CredentialCipher,
	embeddingFactory embeddingport.FactoryRegistry,
	rerankFactory rerankport.FactoryRegistry,
) *ModelConnectionTestService {
	return &ModelConnectionTestService{
		models:           models,
		cipher:           cipher,
		embeddingFactory: embeddingFactory,
		rerankFactory:    rerankFactory,
	}
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
	switch resolved.Model.Type {
	case value.ModelTypeEmbedding:
		return s.testEmbedding(ctx, resolved)
	case value.ModelTypeRerank:
		return s.testRerank(ctx, resolved)
	default:
		return nil, domainerrors.ErrUnsupportedModelType
	}
}

func (s *ModelConnectionTestService) testEmbedding(ctx context.Context, resolved *model.ResolvedModel) (*dto.ConnectionTestResult, error) {
	runtimeClient, err := buildResolvedEmbeddingClient(ctx, resolved, s.cipher, s.embeddingFactory, false)
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
	dimensions := len(result.Vectors[0])
	return &dto.ConnectionTestResult{
		OK:         true,
		Type:       value.ModelTypeEmbedding,
		Dimensions: &dimensions,
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func (s *ModelConnectionTestService) testRerank(ctx context.Context, resolved *model.ResolvedModel) (*dto.ConnectionTestResult, error) {
	if resolved.Model.Type != value.ModelTypeRerank {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	runtimeClient, err := buildResolvedRerankClient(ctx, resolved, s.cipher, s.rerankFactory, false)
	if err != nil {
		return nil, err
	}
	documents := RerankConnectionTestDocuments()
	started := time.Now()
	result, err := runtimeClient.Client.Rerank(ctx, rerankport.RerankInput{
		Query:     RerankConnectionTestQuery,
		Documents: documents,
		TopN:      len(documents),
	})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) != len(documents) {
		return nil, domainerrors.ErrInvalidRerankResponse
	}
	// 验证每个 DocumentID 恰好返回一次，分数有限。
	seen := make(map[string]struct{}, len(result.Items))
	for _, item := range result.Items {
		if _, duplicate := seen[item.DocumentID]; duplicate {
			return nil, domainerrors.ErrInvalidRerankResponse
		}
		seen[item.DocumentID] = struct{}{}
	}
	if len(seen) != len(documents) {
		return nil, domainerrors.ErrInvalidRerankResponse
	}
	resultCount := len(result.Items)
	return &dto.ConnectionTestResult{
		OK:          true,
		Type:        value.ModelTypeRerank,
		ResultCount: &resultCount,
		DurationMS:  time.Since(started).Milliseconds(),
	}, nil
}
