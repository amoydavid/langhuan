//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentTaskStoreScopesLineageAndPersistsTerminalFailure(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID, jobID := uuid.New(), uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "worker.md"); err != nil {
		t.Fatal(err)
	}
	job := &model.Job{
		ID: jobID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		Type: "document_parse_poll", Status: value.JobStatusPending,
		Payload: map[string]any{"index_generation_id": seed.generationID.String()},
	}
	if err := database.WithContext(ctx).Create(jobToRow(job)).Error; err != nil {
		t.Fatal(err)
	}
	store := NewDocumentTaskStore(database)
	if err := store.MarkRunning(ctx, seed.workspaceID, jobID); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSucceeded(ctx, seed.workspaceID, jobID); err != nil {
		t.Fatal(err)
	}
	var completedJobRow JobRow
	if err := database.WithContext(ctx).First(&completedJobRow, "workspace_id = ? AND id = ?", seed.workspaceID, jobID).Error; err != nil {
		t.Fatal(err)
	}
	if completedJobRow.Status != string(value.JobStatusCompleted) || completedJobRow.Attempts != 1 {
		t.Fatalf("completed job = %#v", completedJobRow)
	}
	nextJob, err := store.CreateNextForRevision(
		ctx, seed.workspaceID, seed.kbID, documentID, revisionID, seed.generationID,
		"document_index", map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if nextJob.Status != value.JobStatusPending || nextJob.IndexGenerationID != uuid.Nil ||
		nextJob.Payload["index_generation_id"] != seed.generationID.String() {
		t.Fatalf("next job = %#v", nextJob)
	}
	var gotJob *dto.Job
	var gotRevision *model.DocumentRevision
	var published bool
	err = store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentTaskTx) error {
		var err error
		gotJob, err = tx.GetJob(txCtx, jobID)
		if err != nil {
			return err
		}
		gotRevision, err = tx.GetRevision(txCtx, revisionID)
		if err != nil {
			return err
		}
		published, err = tx.IsRevisionPublished(txCtx, seed.generationID, revisionID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotJob.ID != jobID || gotRevision.ID != revisionID || published {
		t.Fatalf("loaded task = job %v revision %v published %v", gotJob, gotRevision, published)
	}

	if err := store.FailTask(ctx, seed.workspaceID, jobID, revisionID, "invalid_document", "broken"); err != nil {
		t.Fatal(err)
	}
	var jobRow JobRow
	if err := database.WithContext(ctx).First(&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, jobID).Error; err != nil {
		t.Fatal(err)
	}
	var revisionRow DocumentRevisionRow
	if err := database.WithContext(ctx).First(&revisionRow, "workspace_id = ? AND id = ?", seed.workspaceID, revisionID).Error; err != nil {
		t.Fatal(err)
	}
	if jobRow.Status != string(value.JobStatusFailed) || jobRow.ErrorClass != "invalid_document" ||
		revisionRow.Status != string(value.DocumentRevisionFailed) || revisionRow.ErrorClass != "invalid_document" {
		t.Fatalf("failed state = job %#v revision %#v", jobRow, revisionRow)
	}
}
