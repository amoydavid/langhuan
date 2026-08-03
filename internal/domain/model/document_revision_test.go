package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewDocumentRevisionRequiresMatchingDocumentKind(t *testing.T) {
	_, err := NewDocumentRevision(NewDocumentRevisionInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFAQ,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		OriginalFilename: "doc.md", FileType: "markdown", RawStorageKey: "raw/doc",
		ProcessingVersion: 1, Status: value.DocumentRevisionPending,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestNewDocumentRevisionValidatesKindSpecificFields(t *testing.T) {
	_, err := NewDocumentRevision(NewDocumentRevisionInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		Kind: value.DocumentKindFAQ, DocumentKind: value.DocumentKindFAQ,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		FileType: "markdown", ProcessingVersion: 1, Status: value.DocumentRevisionPending,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}
