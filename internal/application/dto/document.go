package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type Document struct {
	ID                 uuid.UUID                `json:"id"`
	WorkspaceID        uuid.UUID                `json:"workspace_id"`
	KnowledgeBaseID    uuid.UUID                `json:"knowledge_base_id"`
	Kind               value.DocumentKind       `json:"kind"`
	Title              string                   `json:"title"`
	SourceURI          string                   `json:"source_uri,omitempty"`
	FileType           string                   `json:"file_type"`
	SourceType         string                   `json:"source_type"`
	Status             value.DocumentStatus     `json:"status"`
	SHA256             string                   `json:"sha256"`
	RawStorageKey      string                   `json:"-"`
	SizeBytes          int64                    `json:"size_bytes"`
	ContentType        string                   `json:"content_type"`
	NormalizedMarkdown string                   `json:"normalized_markdown"`
	Metadata           map[string]any           `json:"metadata"`
	FAQQuestionCount   int                      `json:"faq_question_count,omitempty"`
	ErrorMessage       string                   `json:"error_message"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	ActiveRevision     *DocumentRevisionSummary `json:"active_revision,omitempty"`
}

// DocumentRevisionSummary exposes safe revision facts without object-storage keys.
type DocumentRevisionSummary struct {
	ID               uuid.UUID                    `json:"id"`
	RevisionNo       int64                        `json:"revision_no"`
	Status           value.DocumentRevisionStatus `json:"status"`
	OriginalFilename string                       `json:"original_filename,omitempty"`
	FileType         string                       `json:"file_type,omitempty"`
	ContentType      string                       `json:"content_type,omitempty"`
	SHA256           string                       `json:"sha256,omitempty"`
	SizeBytes        int64                        `json:"size_bytes"`
	CreatedAt        time.Time                    `json:"created_at"`
}

func DocumentFromModel(doc *model.Document) *Document {
	if doc == nil {
		return nil
	}
	result := &Document{
		ID:                 doc.ID,
		WorkspaceID:        doc.WorkspaceID,
		KnowledgeBaseID:    doc.KnowledgeBaseID,
		Kind:               doc.Kind,
		Title:              doc.Title,
		SourceURI:          doc.SourceURI,
		FileType:           doc.FileType,
		SourceType:         doc.SourceType,
		Status:             doc.Status,
		SHA256:             doc.SHA256,
		RawStorageKey:      doc.RawStorageKey,
		SizeBytes:          doc.SizeBytes,
		ContentType:        doc.ContentType,
		NormalizedMarkdown: doc.NormalizedMarkdown,
		Metadata:           doc.Metadata,
		FAQQuestionCount:   doc.FAQQuestionCount,
		ErrorMessage:       doc.ErrorMessage,
		CreatedAt:          doc.CreatedAt,
		UpdatedAt:          doc.UpdatedAt,
	}
	if doc.ActiveRevision != nil {
		applyDocumentRevisionSummary(result, doc.ActiveRevision)
	}
	return result
}

// DocumentFromModelWithRevision builds a safe Document DTO with its current or pending revision summary.
func DocumentFromModelWithRevision(doc *model.Document, revision *model.DocumentRevision) *Document {
	result := DocumentFromModel(doc)
	if result == nil || revision == nil {
		return result
	}
	applyDocumentRevisionSummary(result, revision)
	return result
}

func applyDocumentRevisionSummary(result *Document, revision *model.DocumentRevision) {
	result.FileType = revision.FileType
	result.ContentType = revision.ContentType
	result.SHA256 = revision.SHA256
	result.SizeBytes = revision.SizeBytes
	result.NormalizedMarkdown = revision.NormalizedMarkdown
	result.ErrorMessage = revision.ErrorMessage
	result.ActiveRevision = &DocumentRevisionSummary{
		ID: revision.ID, RevisionNo: revision.RevisionNo, Status: revision.Status,
		OriginalFilename: revision.OriginalFilename, FileType: revision.FileType,
		ContentType: revision.ContentType, SHA256: revision.SHA256, SizeBytes: revision.SizeBytes,
		CreatedAt: revision.CreatedAt,
	}
}
