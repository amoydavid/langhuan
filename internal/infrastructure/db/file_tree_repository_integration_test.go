//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestFileTreeRepositoryMoveRenameAndDeleteRules(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "file-tree")
	provider := createProviderForTest(t, ctx, NewModelProviderRepository(tx), value.ModelScopeWorkspace, &workspaceID, "file-tree")
	embeddingModel := createModelForTest(t, ctx, NewModelRepository(tx), provider.ID, "file-tree", value.ModelStatusActive)
	kbRepo := NewKnowledgeBaseRepository(tx)
	kb, err := appservice.NewKnowledgeBaseService(kbRepo, kbRepo).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "tree", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	document, revision, node, job := newFileIngestAggregate(t, workspaceID, kb.ID, kb.FileTreeRootID, "Guide.pdf")
	ingestStore := NewDocumentIngestDBStore(tx)
	if err := ingestStore.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, ingestTx appservice.DocumentIngestTx) error {
		return ingestTx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
	}); err != nil {
		t.Fatal(err)
	}

	service := appservice.NewFileTreeService(NewFileTreeRepository(tx))
	parent, err := service.CreateFolder(ctx, appservice.CreateFileTreeFolderInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID, ParentID: kb.FileTreeRootID, Name: "Parent",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.CreateFolder(ctx, appservice.CreateFileTreeFolderInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID, ParentID: parent.ID, Name: "Child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Move(ctx, appservice.MoveFileTreeNodeInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID, NodeID: parent.ID, NewParentID: child.ID,
	}); !errors.Is(err, domainerrors.ErrFileTreeCycle) {
		t.Fatalf("move into descendant error = %v", err)
	}
	access := value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}
	if err := service.Delete(ctx, access, kb.ID, parent.ID); !errors.Is(err, domainerrors.ErrFileTreeNotEmpty) {
		t.Fatalf("delete non-empty folder error = %v", err)
	}
	if _, err := service.CreateFolder(ctx, appservice.CreateFileTreeFolderInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID, ParentID: kb.FileTreeRootID, Name: "guide.PDF",
	}); !errors.Is(err, domainerrors.ErrFileTreeNameConflict) {
		t.Fatalf("shared sibling namespace error = %v", err)
	}

	beforeKB, err := kbRepo.Get(ctx, workspaceID, kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	name := "Renamed.pdf"
	if err := service.Update(ctx, appservice.UpdateFileTreeNodeInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID, NodeID: node.ID, Name: &name, ParentID: &parent.ID,
	}); err != nil {
		t.Fatal(err)
	}
	var documentRow DocumentRow
	if err := tx.WithContext(ctx).First(&documentRow, "workspace_id = ? AND id = ?", workspaceID, document.ID).Error; err != nil {
		t.Fatal(err)
	}
	var revisionRow DocumentRevisionRow
	if err := tx.WithContext(ctx).First(&revisionRow, "workspace_id = ? AND id = ?", workspaceID, revision.ID).Error; err != nil {
		t.Fatal(err)
	}
	afterKB, err := kbRepo.Get(ctx, workspaceID, kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if documentRow.Title != name || revisionRow.RawStorageKey == nil || *revisionRow.RawStorageKey != revision.RawStorageKey {
		t.Fatalf("renamed document/revision = %#v / %#v", documentRow, revisionRow)
	}
	if beforeKB.ContentVersion != afterKB.ContentVersion || !sameUUIDPointer(beforeKB.ActiveIndexGenerationID, afterKB.ActiveIndexGenerationID) {
		t.Fatalf("tree mutation changed content/generation: before=%#v after=%#v", beforeKB, afterKB)
	}
	tree, err := service.List(ctx, access, kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Root == nil || tree.Root.Path != "/" || len(tree.Root.Children) == 0 {
		t.Fatalf("tree = %#v", tree)
	}
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	return left != nil && right != nil && *left == *right
}
