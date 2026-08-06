package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type multiRerankResolverStub struct {
	workspaceID uuid.UUID
	client      *ResolvedRerankClient
	err         error
}

func (r *multiRerankResolverStub) Resolve(_ context.Context, workspaceID, _ uuid.UUID) (*ResolvedRerankClient, error) {
	r.workspaceID = workspaceID
	return r.client, r.err
}

func TestApplyMultiKnowledgeRerankUsesWorkspaceAndQuery(t *testing.T) {
	t.Parallel()
	workspaceID, modelID, providerID := uuid.New(), uuid.New(), uuid.New()
	clientSpy := &recordingRerankClientForSearch{scores: []float64{0.9}}
	resolver := &multiRerankResolverStub{client: &ResolvedRerankClient{
		Client: clientSpy, ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", MaxDocumentChars: 8192,
	}}
	service := &MultiKnowledgeSearchService{rerankResolver: resolver, logger: slog.Default()}
	results, err := service.applyMultiKnowledgeRerank(context.Background(), workspaceID, "查询问题", []*dto.SearchResult{{ChunkID: uuid.New(), Content: "候选正文"}}, &model.RerankSnapshot{
		ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFail,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolver.workspaceID != workspaceID || clientSpy.input.Query != "查询问题" || results[0].RankingStage != value.RankingStageRerank {
		t.Fatalf("workspace=%s query=%q stage=%s", resolver.workspaceID, clientSpy.input.Query, results[0].RankingStage)
	}
}

func TestApplyMultiKnowledgeRerankFailPropagatesError(t *testing.T) {
	t.Parallel()
	workspaceID, modelID, providerID := uuid.New(), uuid.New(), uuid.New()
	resolver := &multiRerankResolverStub{client: &ResolvedRerankClient{
		Client: &recordingRerankClientForSearch{err: domainerrors.ErrRerankUnavailable}, ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", MaxDocumentChars: 8192,
	}}
	service := &MultiKnowledgeSearchService{rerankResolver: resolver, logger: slog.Default()}
	_, err := service.applyMultiKnowledgeRerank(context.Background(), workspaceID, "q", []*dto.SearchResult{{ChunkID: uuid.New(), Content: "doc"}}, &model.RerankSnapshot{
		ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFail,
	})
	if !errors.Is(err, domainerrors.ErrRerankUnavailable) {
		t.Fatalf("err = %v, want rerank unavailable", err)
	}
}

func TestApplyMultiKnowledgeRerankFallbackReturnsRRF(t *testing.T) {
	t.Parallel()
	workspaceID, modelID, providerID := uuid.New(), uuid.New(), uuid.New()
	resolver := &multiRerankResolverStub{client: &ResolvedRerankClient{
		Client: &recordingRerankClientForSearch{err: domainerrors.ErrRerankUnavailable}, ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", MaxDocumentChars: 8192,
	}}
	service := &MultiKnowledgeSearchService{rerankResolver: resolver, logger: slog.Default()}
	results, err := service.applyMultiKnowledgeRerank(context.Background(), workspaceID, "q", []*dto.SearchResult{{ChunkID: uuid.New(), Content: "doc"}}, &model.RerankSnapshot{
		ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFallback,
	})
	if err != nil || results[0].RankingStage != value.RankingStageRRFFallback {
		t.Fatalf("results=%#v err=%v", results, err)
	}
}
