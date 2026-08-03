//go:build integration

package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentRevisionRepositoryCompletesParseWithoutPublishing(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "parse.md"); err != nil {
		t.Fatal(err)
	}
	repository := NewDocumentRevisionRepository(database)
	markdown := "正文"
	manifest := model.ParseManifest{
		Version: model.CurrentParseManifestVersion, Parser: "markdown", ParserVersion: 1,
		Blocks: []model.ParsedBlock{{
			Sequence: 0, Kind: model.BlockKindParagraph,
			NormalizedStart: 0, NormalizedEnd: len(markdown),
			SourceAnchor: value.SourceAnchor{SourceType: "markdown"},
		}},
	}

	if err := repository.CompleteParse(ctx, seed.workspaceID, revisionID, markdown, manifest); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, seed.workspaceID, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != value.DocumentRevisionReady || got.NormalizedMarkdown != markdown || got.ParseManifest == nil || got.CompletedAt == nil {
		t.Fatalf("revision = %#v", got)
	}
	if err := repository.CompleteParse(ctx, seed.workspaceID, revisionID, "different", manifest); err != nil {
		t.Fatal(err)
	}
	retried, err := repository.Get(ctx, seed.workspaceID, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.NormalizedMarkdown != markdown {
		t.Fatalf("retry changed immutable parse result to %q", retried.NormalizedMarkdown)
	}
	var document DocumentRow
	if err := database.WithContext(ctx).First(&document, "workspace_id = ? AND id = ?", seed.workspaceID, documentID).Error; err != nil {
		t.Fatal(err)
	}
	if document.ActiveRevisionID != nil {
		t.Fatalf("active_revision_id = %v, want nil before publish", document.ActiveRevisionID)
	}
	if _, err := repository.Get(ctx, uuid.New(), revisionID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace Get error = %v, want ErrNotFound", err)
	}
}

func TestIndexGenerationRepositoryGetsWorkspaceScopedSnapshot(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewIndexGenerationRepository(database)

	generation, err := repository.Get(ctx, seed.workspaceID, seed.generationID)
	if err != nil {
		t.Fatal(err)
	}
	if generation.ID != seed.generationID || generation.KnowledgeBaseID != seed.kbID || generation.ChunkerVersion != 1 {
		t.Fatalf("generation = %#v", generation)
	}
	if _, err := repository.Get(ctx, uuid.New(), seed.generationID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace Get error = %v, want ErrNotFound", err)
	}
}

func TestIndexGenerationRepositorySetsTenantLocalContext(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewIndexGenerationRepository(database)
	callbackName := "test:index_generation_workspace_context:" + uuid.NewString()
	if err := database.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		var workspaceID string
		if err := tx.Statement.ConnPool.QueryRowContext(
			ctx, "SELECT COALESCE(current_setting('app.workspace_id', true), '')",
		).Scan(&workspaceID); err != nil {
			tx.AddError(fmt.Errorf("读取 Workspace 数据库上下文失败: %w", err))
			return
		}
		if workspaceID != seed.workspaceID.String() {
			tx.AddError(fmt.Errorf("app.workspace_id = %q, want %s", workspaceID, seed.workspaceID))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(callbackName) })

	if _, err := repository.Get(ctx, seed.workspaceID, seed.generationID); err != nil {
		t.Fatal(err)
	}
}
