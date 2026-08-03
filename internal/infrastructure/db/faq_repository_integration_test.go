//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestFAQRepositoryWritesAndReadsCompleteAggregate(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewFAQRepository(database)
	document, faq, job := testFAQPersistenceAggregate(t, seed)

	err := repository.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.FAQRevisionTx) error {
		return tx.CreateFAQRevisionAggregate(txCtx, document, faq, job)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.GetFAQRevision(ctx, seed.workspaceID, faq.DocumentRevision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer != faq.Answer || len(got.Questions) != 2 || got.Questions[0].Sequence != 0 || got.Questions[1].Sequence != 1 {
		t.Fatalf("FAQ = %#v", got)
	}
	if _, err := repository.GetFAQRevision(ctx, uuid.New(), faq.DocumentRevision.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace Get error = %v, want ErrNotFound", err)
	}
}

func TestFAQRepositoryRollsBackIncompleteAggregate(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewFAQRepository(database)
	document, faq, job := testFAQPersistenceAggregate(t, seed)
	faq.Questions[1].NormalizedQuestion = faq.Questions[0].NormalizedQuestion

	err := repository.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.FAQRevisionTx) error {
		return tx.CreateFAQRevisionAggregate(txCtx, document, faq, job)
	})
	if err == nil {
		t.Fatal("CreateFAQRevisionAggregate error = nil")
	}
	for table, predicate := range map[string]string{
		"documents": "id = ?", "document_revisions": "id = ?",
		"faq_revision_contents": "document_revision_id = ?", "jobs": "id = ?",
	} {
		id := document.ID
		if table == "document_revisions" || table == "faq_revision_contents" {
			id = faq.DocumentRevision.ID
		} else if table == "jobs" {
			id = job.ID
		}
		var count int64
		if err := database.WithContext(ctx).Table(table).Where(predicate, id).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d, want rollback", table, count)
		}
	}
}

func testFAQPersistenceAggregate(t *testing.T, seed knowledgeSchemaSeed) (*model.Document, *model.FAQRevision, *model.Job) {
	t.Helper()
	document, err := model.NewDocumentIdentity(
		seed.workspaceID, seed.kbID, value.DocumentKindFAQ, "退款", "api", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: document.ID,
		Kind: value.DocumentKindFAQ, DocumentKind: value.DocumentKindFAQ,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		ProcessingVersion: 1, Status: value.DocumentRevisionReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	revision.CompletedAt = &now
	faq, err := model.NewFAQRevision(model.NewFAQRevisionInput{
		DocumentRevision: revision, Answer: "请在订单页申请退款。",
		Questions: []string{"如何退款？", "退款流程是什么？"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: "document_index", Status: value.JobStatusPending,
		Payload: map[string]any{"index_generation_id": seed.generationID.String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document, faq, job
}
