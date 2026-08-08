package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewDocumentRequiresWorkspaceScopedKnowledgeBase(t *testing.T) {
	doc, err := NewDocument(NewDocumentInput{
		KnowledgeBaseID: uuid.New(),
		Title:           "a.pdf",
		FileType:        "pdf",
		SourceType:      "upload",
		Status:          value.DocumentStatusPending,
		SHA256:          "abc",
		RawStorageKey:   "raw/a.pdf",
		SizeBytes:       3,
		ContentType:     "application/pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID == uuid.Nil {
		t.Fatal("document id should not be nil")
	}
}

func TestNewDocumentValidatesInput(t *testing.T) {
	valid := NewDocumentInput{
		KnowledgeBaseID: uuid.New(),
		Title:           "a.pdf",
		FileType:        "pdf",
		SourceType:      "upload",
		Status:          value.DocumentStatusPending,
		SHA256:          "abc",
		RawStorageKey:   "raw/a.pdf",
		SizeBytes:       3,
		ContentType:     "application/pdf",
	}

	tests := []struct {
		name  string
		input NewDocumentInput
	}{
		{name: "knowledge base", input: func() NewDocumentInput { in := valid; in.KnowledgeBaseID = uuid.Nil; return in }()},
		{name: "title", input: func() NewDocumentInput { in := valid; in.Title = " "; return in }()},
		{name: "file type", input: func() NewDocumentInput { in := valid; in.FileType = ""; return in }()},
		{name: "source type", input: func() NewDocumentInput { in := valid; in.SourceType = ""; return in }()},
		{name: "status", input: func() NewDocumentInput { in := valid; in.Status = ""; return in }()},
		{name: "sha256", input: func() NewDocumentInput { in := valid; in.SHA256 = ""; return in }()},
		{name: "raw storage key", input: func() NewDocumentInput { in := valid; in.RawStorageKey = ""; return in }()},
		{name: "negative size", input: func() NewDocumentInput { in := valid; in.SizeBytes = -1; return in }()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewDocument(tt.input); !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestNewDocumentDefaultsMetadataToEmptyMap(t *testing.T) {
	doc, err := NewDocument(NewDocumentInput{
		KnowledgeBaseID: uuid.New(),
		Title:           "a.pdf",
		FileType:        "pdf",
		SourceType:      "upload",
		Status:          value.DocumentStatusPending,
		SHA256:          "abc",
		RawStorageKey:   "raw/a.pdf",
		SizeBytes:       3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Metadata == nil {
		t.Fatal("metadata should default to an empty map")
	}
}

func TestNewDocumentIdentityWithExternalSetsExternalID(t *testing.T) {
	workspaceID, kbID := uuid.New(), uuid.New()
	externalID := "feishu-docx-external-123"
	document, err := NewDocumentIdentityWithExternal(
		workspaceID, kbID, value.DocumentKindFile, "飞书文档", "feishu", "", externalID, nil,
	)
	if err != nil {
		t.Fatalf("NewDocumentIdentityWithExternal error = %v", err)
	}
	if document.ExternalID != externalID {
		t.Fatalf("ExternalID = %q, want %q", document.ExternalID, externalID)
	}
	if document.SourceType != "feishu" {
		t.Fatalf("SourceType = %q, want feishu", document.SourceType)
	}
	if document.WorkspaceID != workspaceID || document.KnowledgeBaseID != kbID {
		t.Fatalf("lineage = %s/%s", document.WorkspaceID, document.KnowledgeBaseID)
	}
}

func TestNewDocumentIdentityWithExternalTrimsWhitespace(t *testing.T) {
	document, err := NewDocumentIdentityWithExternal(
		uuid.New(), uuid.New(), value.DocumentKindFile, "t", "feishu", "", "  spaced-external-id  ", nil,
	)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if document.ExternalID != "spaced-external-id" {
		t.Fatalf("ExternalID = %q, want trimmed", document.ExternalID)
	}
}

func TestNewDocumentIdentityWithExternalPropagatesValidationError(t *testing.T) {
	if _, err := NewDocumentIdentityWithExternal(
		uuid.Nil, uuid.New(), value.DocumentKindFile, "t", "feishu", "", "ext", nil,
	); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestNewDocumentPropagatesContentHash 验证 NewDocument 把 NewDocumentInput.ContentHash
// 写入 Document.ContentHash 字段，供来源同步去重使用。
func TestNewDocumentPropagatesContentHash(t *testing.T) {
	const hash = "sha256:deadbeef"
	doc, err := NewDocument(NewDocumentInput{
		KnowledgeBaseID: uuid.New(),
		Title:           "a.pdf", FileType: "pdf", SourceType: "upload",
		Status: value.DocumentStatusPending, SHA256: "abc", RawStorageKey: "raw/a.pdf",
		SizeBytes: 3, ContentType: "application/pdf",
		ContentHash: hash,
	})
	if err != nil {
		t.Fatalf("NewDocument error = %v", err)
	}
	if doc.ContentHash != hash {
		t.Fatalf("ContentHash = %q, want %q", doc.ContentHash, hash)
	}
}

// TestNewDocumentDefaultsContentHashToEmpty 验证 ContentHash 默认为空（向后兼容）。
func TestNewDocumentDefaultsContentHashToEmpty(t *testing.T) {
	doc, err := NewDocument(NewDocumentInput{
		KnowledgeBaseID: uuid.New(),
		Title:           "a.pdf", FileType: "pdf", SourceType: "upload",
		Status: value.DocumentStatusPending, SHA256: "abc", RawStorageKey: "raw/a.pdf",
		SizeBytes: 3,
	})
	if err != nil {
		t.Fatalf("NewDocument error = %v", err)
	}
	if doc.ContentHash != "" {
		t.Fatalf("ContentHash = %q, want empty", doc.ContentHash)
	}
}
