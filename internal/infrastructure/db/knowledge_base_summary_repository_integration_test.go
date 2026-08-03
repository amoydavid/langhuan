//go:build integration

package db

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestKnowledgeBaseSummaryRepositoryReadsScopedReadableFacts(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	if err := database.WithContext(ctx).Model(&ModelRow{}).
		Where("id = ?", seed.modelID).
		Updates(map[string]any{"display_name": "Text Embedding V4"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).
		Updates(map[string]any{"name": "产品文档", "content_version": 3}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).
		Updates(map[string]any{
			"created_at": now.Add(-time.Hour), "document_count": 2, "chunk_count": 8,
			"indexed_count": 7, "source_content_version": 3, "indexed_content_version": 3,
		}).Error; err != nil {
		t.Fatal(err)
	}

	fileID, fileRevisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, fileID, fileRevisionID, "title-from-document.md"); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&FileTreeNodeRow{}).
		Where("workspace_id = ? AND document_id = ?", seed.workspaceID, fileID).
		Update("name", "installation.md").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, fileID).
		Updates(map[string]any{"status": string(value.DocumentStatusFailed), "updated_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	faqID := uuid.New()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status, created_at, updated_at) VALUES (?, ?, ?, 'faq', '退款政策', 'api', 'processing', ?, ?)",
		faqID, seed.workspaceID, seed.kbID, now, now,
	).Error; err != nil {
		t.Fatal(err)
	}

	candidateID := uuid.New()
	if err := database.WithContext(ctx).Create(&IndexGenerationRow{
		ID: candidateID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		BaseGenerationID: &seed.generationID, EmbeddingModelID: seed.modelID, ProviderID: seed.providerID,
		ModelName: "text-embedding-v4", EmbeddingDimension: 1024, ModelConfigHash: "candidate-model",
		ChunkerVersion: 1, ChunkingConfig: JSONMap{}, RetrievalConfig: JSONMap{}, ConfigHash: "candidate-config",
		SourceContentVersion: 3, IndexedContentVersion: 3, Status: string(value.IndexGenerationReady), CreatedAt: now,
		ManualEditDisposition: string(value.ManualEditNotApplicable),
	}).Error; err != nil {
		t.Fatal(err)
	}
	jobID := uuid.New()
	if err := database.WithContext(ctx).Create(&JobRow{
		ID: jobID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: &fileID, DocumentRevisionID: &fileRevisionID,
		Type: "document_parse_start", Status: string(value.JobStatusFailed), Attempts: 2,
		Payload: JSONMap{"credential": "must-not-leave-repository"}, ErrorClass: "provider_error",
		ErrorMessage: "Authorization: Bearer secret", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	repository := NewKnowledgeBaseSummaryRepository(database)
	facts, err := repository.GetKnowledgeBaseSummaryFacts(ctx, seed.workspaceID, seed.kbID)
	if err != nil {
		t.Fatal(err)
	}
	if facts.KnowledgeBaseName != "产品文档" || facts.TotalDocuments != 2 || facts.FileDocuments != 1 || facts.FAQDocuments != 1 || facts.FailedDocuments != 1 || facts.ProcessingDocuments != 1 {
		t.Fatalf("summary facts = %#v", facts)
	}
	if facts.ActiveGeneration == nil || facts.ActiveGeneration.ID != seed.generationID || facts.CandidateGeneration == nil || facts.CandidateGeneration.ID != candidateID || facts.CandidateGeneration.ModelDisplayName != "Text Embedding V4" {
		t.Fatalf("generation facts = active %#v candidate %#v", facts.ActiveGeneration, facts.CandidateGeneration)
	}
	if len(facts.RecentJobs) != 1 || facts.RecentJobs[0].ID != jobID || facts.RecentJobs[0].TargetDisplayName != "installation.md" || facts.RecentJobs[0].ErrorMessage != "Authorization: Bearer secret" {
		t.Fatalf("recent jobs = %#v", facts.RecentJobs)
	}
	if len(facts.Blockers) == 0 || facts.Blockers[0].ResourceDisplayName != "installation.md" {
		t.Fatalf("blockers = %#v", facts.Blockers)
	}

	jobs, err := repository.ListKnowledgeBaseJobFacts(ctx, seed.workspaceID, seed.kbID, appservice.KnowledgeBaseJobFactsFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != jobID || jobs[0].TargetDisplayName != "installation.md" {
		t.Fatalf("jobs = %#v", jobs)
	}
	otherWorkspaceID := createWorkspaceRow(t, ctx, database, "summary-other-"+uuid.NewString())
	if _, err := repository.GetKnowledgeBaseSummaryFacts(ctx, otherWorkspaceID, seed.kbID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace summary error = %v", err)
	}
	if _, err := repository.ListKnowledgeBaseJobFacts(ctx, otherWorkspaceID, seed.kbID, appservice.KnowledgeBaseJobFactsFilter{Limit: 20}); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace jobs error = %v", err)
	}
}
