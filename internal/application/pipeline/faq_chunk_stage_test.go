package pipeline

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestFAQChunkStageBuildsFixedIdempotentSet(t *testing.T) {
	workspaceID, knowledgeBaseID, documentID, revisionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	faq := &model.FAQRevision{
		DocumentRevision: &model.DocumentRevision{
			ID: revisionID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
			DocumentID: documentID, Kind: value.DocumentKindFAQ, RevisionNo: 1,
			Status: value.DocumentRevisionReady,
		},
		Answer: "请在订单页申请退款。",
		Questions: []model.FAQRevisionQuestion{
			{Sequence: 0, Question: "如何退款？"},
			{Sequence: 1, Question: "退款流程是什么？"},
		},
	}
	sets := &fakeChunkSetRepository{}
	stage := NewFAQChunkStage(&fakeFAQRevisionGetter{faq: faq}, sets)

	firstID, err := stage.Build(context.Background(), workspaceID, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := stage.Build(context.Background(), workspaceID, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == uuid.Nil || secondID != firstID || sets.completeCalls != 1 {
		t.Fatalf("set IDs=%s/%s complete=%d", firstID, secondID, sets.completeCalls)
	}
	if sets.set.Strategy != value.ChunkStrategyFAQ || sets.set.ChunkCount != 1 || len(sets.chunks) != 1 || len(sets.revisions) != 1 {
		t.Fatalf("FAQ set=%#v chunks=%#v revisions=%#v", sets.set, sets.chunks, sets.revisions)
	}
	chunk, revision := sets.chunks[0], sets.revisions[0]
	if chunk.SourceContent != "Q: 如何退款？\nQ: 退款流程是什么？\nA: 请在订单页申请退款。" {
		t.Fatalf("source content = %q", chunk.SourceContent)
	}
	if revision.Content != "请在订单页申请退款。" || revision.EmbeddingContent != "如何退款？\n退款流程是什么？" {
		t.Fatalf("revision = %#v", revision)
	}
	if revision.EditSource != value.ChunkEditSourceSystem || revision.ChunkID != chunk.ID ||
		chunk.ActiveRevisionID == nil || *chunk.ActiveRevisionID != revision.ID {
		t.Fatalf("FAQ chunk lineage = %#v / %#v", chunk, revision)
	}
}

func TestDocumentPipelineRoutesFAQToFixedChunkStage(t *testing.T) {
	workspaceID, knowledgeBaseID, documentID, revisionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	documentRevision := &model.DocumentRevision{
		ID: revisionID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, Kind: value.DocumentKindFAQ, RevisionNo: 1,
		Status: value.DocumentRevisionReady,
	}
	faq := &model.FAQRevision{
		DocumentRevision: documentRevision,
		Answer:           "固定回答",
		Questions: []model.FAQRevisionQuestion{
			{Sequence: 0, Question: "第一个问题？"},
			{Sequence: 1, Question: "第二个问题？"},
		},
	}
	sets := &fakeChunkSetRepository{}
	documentPipeline := NewDocumentPipeline(DocumentPipelineDeps{
		Revisions:    &fakeRevisionRepository{revision: documentRevision},
		ChunkSets:    sets,
		FAQRevisions: &fakeFAQRevisionGetter{faq: faq},
	})

	chunkSetID, err := documentPipeline.RunChunk(context.Background(), workspaceID, revisionID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if chunkSetID == uuid.Nil || sets.set == nil || sets.set.Strategy != value.ChunkStrategyFAQ || sets.completeCalls != 1 {
		t.Fatalf("FAQ routed set = %#v id=%s complete=%d", sets.set, chunkSetID, sets.completeCalls)
	}
}

type fakeFAQRevisionGetter struct {
	faq *model.FAQRevision
}

func (g *fakeFAQRevisionGetter) GetFAQRevision(
	_ context.Context,
	workspaceID, revisionID uuid.UUID,
) (*model.FAQRevision, error) {
	if g.faq == nil || g.faq.DocumentRevision.WorkspaceID != workspaceID ||
		g.faq.DocumentRevision.ID != revisionID {
		return nil, domainerrors.ErrNotFound
	}
	return g.faq, nil
}
