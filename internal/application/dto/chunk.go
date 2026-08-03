package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// Chunk exposes stable source lineage and its current effective revision.
type Chunk struct {
	ID                 uuid.UUID      `json:"id"`
	WorkspaceID        uuid.UUID      `json:"workspace_id"`
	KnowledgeBaseID    uuid.UUID      `json:"knowledge_base_id"`
	DocumentID         uuid.UUID      `json:"document_id"`
	DocumentRevisionID uuid.UUID      `json:"document_revision_id"`
	ChunkSetID         uuid.UUID      `json:"chunk_set_id"`
	Sequence           int            `json:"sequence"`
	SourceContent      string         `json:"source_content"`
	SourceAnchor       map[string]any `json:"source_anchor"`
	Metadata           map[string]any `json:"metadata"`
	ActiveRevision     *ChunkRevision `json:"active_revision,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
}

// ChunkRevision exposes one immutable effective text revision.
type ChunkRevision struct {
	ID                uuid.UUID                 `json:"id"`
	ChunkID           uuid.UUID                 `json:"chunk_id"`
	RevisionNo        int64                     `json:"revision_no"`
	BaseRevisionID    *uuid.UUID                `json:"base_revision_id,omitempty"`
	Content           string                    `json:"content"`
	ContextHeader     string                    `json:"context_header"`
	Enabled           bool                      `json:"enabled"`
	Status            value.ChunkRevisionStatus `json:"status"`
	EditSource        value.ChunkEditSource     `json:"edit_source"`
	EditorUserID      *uuid.UUID                `json:"editor_user_id,omitempty"`
	EditorDisplayName string                    `json:"editor_display_name"`
	ErrorMessage      string                    `json:"error_message,omitempty"`
	CreatedAt         time.Time                 `json:"created_at"`
	IndexedAt         *time.Time                `json:"indexed_at,omitempty"`
}

// ChunkFromModel builds a safe Chunk DTO with its active revision.
func ChunkFromModel(chunk *model.Chunk, revision *model.ChunkRevision) *Chunk {
	if chunk == nil {
		return nil
	}
	return &Chunk{
		ID: chunk.ID, WorkspaceID: chunk.WorkspaceID, KnowledgeBaseID: chunk.KnowledgeBaseID,
		DocumentID: chunk.DocumentID, DocumentRevisionID: chunk.DocumentRevisionID,
		ChunkSetID: chunk.ChunkSetID, Sequence: chunk.Sequence, SourceContent: chunk.SourceContent,
		SourceAnchor: map[string]any{
			"source_type":  chunk.SourceAnchor.SourceType,
			"offset_start": chunk.SourceAnchor.OffsetStart, "offset_end": chunk.SourceAnchor.OffsetEnd,
			"line_start": chunk.SourceAnchor.LineStart, "line_end": chunk.SourceAnchor.LineEnd,
			"sheet": chunk.SourceAnchor.Sheet, "header_row": chunk.SourceAnchor.HeaderRow,
			"row_start": chunk.SourceAnchor.RowStart, "row_end": chunk.SourceAnchor.RowEnd,
			"column_start": chunk.SourceAnchor.ColumnStart, "column_end": chunk.SourceAnchor.ColumnEnd,
			"paragraph_start": chunk.SourceAnchor.ParagraphStart, "paragraph_end": chunk.SourceAnchor.ParagraphEnd,
			"table_index": chunk.SourceAnchor.TableIndex,
		},
		Metadata: chunk.Metadata, ActiveRevision: ChunkRevisionFromModel(revision), CreatedAt: chunk.CreatedAt,
	}
}

// ChunkRevisionFromModel builds one revision DTO.
func ChunkRevisionFromModel(revision *model.ChunkRevision) *ChunkRevision {
	if revision == nil {
		return nil
	}
	return &ChunkRevision{
		ID: revision.ID, ChunkID: revision.ChunkID, RevisionNo: revision.RevisionNo,
		BaseRevisionID: revision.BaseRevisionID, Content: revision.Content,
		ContextHeader: revision.ContextHeader, Enabled: revision.Enabled, Status: revision.Status,
		EditSource: revision.EditSource, EditorUserID: revision.EditorUserID,
		ErrorMessage: revision.ErrorMessage, CreatedAt: revision.CreatedAt, IndexedAt: revision.IndexedAt,
	}
}
