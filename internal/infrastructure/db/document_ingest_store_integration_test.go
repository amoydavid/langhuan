//go:build integration

package db

import (
	"context"
	"testing"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentIngestStoreFailCreatedIngestUpdatesAggregateAtomically(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	document, revision, node, job := newFileIngestAggregate(
		t, seed.workspaceID, seed.kbID, seed.rootID, "queue-failure.md",
	)
	store := NewDocumentIngestDBStore(database)
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentIngestTx) error {
			return tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatal(err)
	}

	if err := store.FailCreatedIngest(
		ctx, seed.workspaceID, document.ID, revision.ID, job.ID,
		"enqueue_error", "queue unavailable",
	); err != nil {
		t.Fatal(err)
	}

	var documentRow DocumentRow
	if err := database.WithContext(ctx).First(&documentRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatal(err)
	}
	var revisionRow DocumentRevisionRow
	if err := database.WithContext(ctx).First(&revisionRow, "workspace_id = ? AND id = ?", seed.workspaceID, revision.ID).Error; err != nil {
		t.Fatal(err)
	}
	var jobRow JobRow
	if err := database.WithContext(ctx).First(&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if documentRow.Status != string(value.DocumentStatusFailed) {
		t.Fatalf("document status = %q, want failed", documentRow.Status)
	}
	if revisionRow.Status != string(value.DocumentRevisionFailed) || revisionRow.ErrorClass != "enqueue_error" || revisionRow.ErrorMessage != "queue unavailable" || revisionRow.CompletedAt == nil {
		t.Fatalf("revision failure = %#v", revisionRow)
	}
	if jobRow.Status != string(value.JobStatusFailed) || jobRow.ErrorClass != "enqueue_error" || jobRow.ErrorMessage != "queue unavailable" {
		t.Fatalf("job failure = %#v", jobRow)
	}
}
