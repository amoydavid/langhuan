package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewUserChunkRevisionRequiresBaseAndEditor(t *testing.T) {
	_, err := NewChunkRevision(NewChunkRevisionInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(), ChunkID: uuid.New(),
		RevisionNo: 2, Content: "edited", EmbeddingContent: "edited",
		Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceUser,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}
