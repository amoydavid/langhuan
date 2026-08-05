package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func sameRerank() *model.RerankSnapshot {
	return &model.RerankSnapshot{
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank",
		ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFallback,
	}
}

func otherRerank() *model.RerankSnapshot {
	return &model.RerankSnapshot{
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank-other",
		ModelConfigHash: "otherhash", CandidateTopK: 100, FailureMode: value.RerankFailureFail,
	}
}

func snapshotWith(kbID uuid.UUID, gen *model.IndexGeneration) knowledgeBaseSearchSnapshot {
	return knowledgeBaseSearchSnapshot{knowledgeBaseID: kbID, name: "kb", generation: gen}
}

func TestPlanMultiKnowledgeRerankAllDisabled(t *testing.T) {
	t.Parallel()
	kb1, kb2 := uuid.New(), uuid.New()
	snapshots := map[uuid.UUID]knowledgeBaseSearchSnapshot{
		kb1: snapshotWith(kb1, &model.IndexGeneration{Rerank: nil}),
		kb2: snapshotWith(kb2, &model.IndexGeneration{Rerank: nil}),
	}
	plan, err := planMultiKnowledgeRerank(snapshots)
	if err != nil || plan.enabled {
		t.Fatalf("plan = %+v err = %v", plan, err)
	}
}

func TestPlanMultiKnowledgeRerankSameSnapshot(t *testing.T) {
	t.Parallel()
	kb1, kb2 := uuid.New(), uuid.New()
	rerank := sameRerank()
	snapshots := map[uuid.UUID]knowledgeBaseSearchSnapshot{
		kb1: snapshotWith(kb1, &model.IndexGeneration{Rerank: rerank}),
		kb2: snapshotWith(kb2, &model.IndexGeneration{Rerank: rerank.Clone()}),
	}
	plan, err := planMultiKnowledgeRerank(snapshots)
	if err != nil || !plan.enabled || plan.snapshot == nil {
		t.Fatalf("plan = %+v err = %v", plan, err)
	}
}

func TestPlanMultiKnowledgeRerankMixedEnabledConflict(t *testing.T) {
	t.Parallel()
	kb1, kb2 := uuid.New(), uuid.New()
	rerank := sameRerank()
	snapshots := map[uuid.UUID]knowledgeBaseSearchSnapshot{
		kb1: snapshotWith(kb1, &model.IndexGeneration{Rerank: rerank}),
		kb2: snapshotWith(kb2, &model.IndexGeneration{Rerank: nil}),
	}
	_, err := planMultiKnowledgeRerank(snapshots)
	if !errors.Is(err, domainerrors.ErrRerankConfigurationConflict) {
		t.Fatalf("mixed conflict err = %v", err)
	}
}

func TestPlanMultiKnowledgeRerankDifferentSnapshotConflict(t *testing.T) {
	t.Parallel()
	kb1, kb2 := uuid.New(), uuid.New()
	snapshots := map[uuid.UUID]knowledgeBaseSearchSnapshot{
		kb1: snapshotWith(kb1, &model.IndexGeneration{Rerank: sameRerank()}),
		kb2: snapshotWith(kb2, &model.IndexGeneration{Rerank: otherRerank()}),
	}
	_, err := planMultiKnowledgeRerank(snapshots)
	if !errors.Is(err, domainerrors.ErrRerankConfigurationConflict) {
		t.Fatalf("different snapshot conflict err = %v", err)
	}
}
