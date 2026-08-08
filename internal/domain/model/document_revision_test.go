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

// validRevisionInput 返回一份通过校验的 NewDocumentRevisionInput（File kind）。
func validRevisionInput() NewDocumentRevisionInput {
	return NewDocumentRevisionInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		OriginalFilename: "doc.md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: "raw/doc", SHA256: "abc",
		ProcessingVersion: 1, Status: value.DocumentRevisionPending,
	}
}

// TestNewDocumentRevisionWithIDUsesExplicitID 验证显式入口使用传入的 ID。
func TestNewDocumentRevisionWithIDUsesExplicitID(t *testing.T) {
	revisionID := uuid.New()
	rev, err := NewDocumentRevisionWithID(revisionID, validRevisionInput())
	if err != nil {
		t.Fatalf("NewDocumentRevisionWithID error = %v", err)
	}
	if rev.ID != revisionID {
		t.Fatalf("ID = %s, want %s", rev.ID, revisionID)
	}
}

// TestNewDocumentRevisionWithIDRejectsNilID 验证显式入口拒绝 uuid.Nil。
func TestNewDocumentRevisionWithIDRejectsNilID(t *testing.T) {
	_, err := NewDocumentRevisionWithID(uuid.Nil, validRevisionInput())
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestNewDocumentRevisionDelegatesAndGeneratesID 验证兼容入口 NewDocumentRevision
// 委托给 WithID 并生成非空 UUID。
func TestNewDocumentRevisionDelegatesAndGeneratesID(t *testing.T) {
	rev, err := NewDocumentRevision(validRevisionInput())
	if err != nil {
		t.Fatalf("NewDocumentRevision error = %v", err)
	}
	if rev.ID == uuid.Nil {
		t.Fatalf("generated revision ID should not be nil")
	}
}
