package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

const (
	defaultDocumentChunksLimit = 50
	maxDocumentChunksLimit     = 200
)

// DocumentChunkFacts contains one Chunk and its current effective revision.
type DocumentChunkFacts struct {
	Chunk          *model.Chunk
	ActiveRevision ChunkRevisionFacts
}

// DocumentChunkFactsPage carries authoritative active lineage plus ordered rows.
type DocumentChunkFactsPage struct {
	GenerationID       uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	Items              []DocumentChunkFacts
}

// DocumentChunkFactsFilter is the repository-facing stable seek filter.
type DocumentChunkFactsFilter struct {
	Enabled       *bool
	AfterSequence *int
	AfterID       *uuid.UUID
	Limit         int
}

// DocumentChunksInput is the protocol-neutral effective Chunk list request.
type DocumentChunksInput struct {
	WorkspaceID, KnowledgeBaseID, DocumentID uuid.UUID
	Enabled                                  *bool
	Cursor                                   string
	Limit                                    int
}

// DocumentChunksStore reads effective Chunk facts without using retrieval entries.
type DocumentChunksStore interface {
	ListDocumentChunkFacts(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		DocumentChunkFactsFilter,
	) (*DocumentChunkFactsPage, error)
}

// DocumentChunksService lists the current effective ChunkSet for one Document.
type DocumentChunksService struct {
	store DocumentChunksStore
}

// NewDocumentChunksService creates the effective Document Chunk query.
func NewDocumentChunksService(store DocumentChunksStore) *DocumentChunksService {
	return &DocumentChunksService{store: store}
}

// List returns a stable sequence/id page, including disabled active revisions.
func (s *DocumentChunksService) List(ctx context.Context, input DocumentChunksInput) (*dto.DocumentChunkPage, error) {
	if s.store == nil || input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.DocumentID == uuid.Nil {
		return nil, fmt.Errorf("%w: Document Chunk lineage 无效", domainerrors.ErrValidation)
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultDocumentChunksLimit
	}
	if limit < 1 || limit > maxDocumentChunksLimit {
		return nil, fmt.Errorf("%w: Document Chunk limit 必须是 1 到 %d", domainerrors.ErrValidation, maxDocumentChunksLimit)
	}
	filter := DocumentChunkFactsFilter{Enabled: input.Enabled, Limit: limit + 1}
	if strings.TrimSpace(input.Cursor) != "" {
		cursor, err := decodeDocumentChunksCursor(input.Cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: Document Chunk cursor 无效", domainerrors.ErrValidation)
		}
		filter.AfterSequence = &cursor.Sequence
		filter.AfterID = &cursor.ID
	}
	facts, err := s.store.ListDocumentChunkFacts(
		ctx, input.WorkspaceID, input.KnowledgeBaseID, input.DocumentID, filter,
	)
	if err != nil {
		return nil, err
	}
	if facts == nil || facts.GenerationID == uuid.Nil || facts.DocumentRevisionID == uuid.Nil || facts.ChunkSetID == uuid.Nil {
		return nil, fmt.Errorf("Document Chunk facts lineage 不完整")
	}
	pageSize := len(facts.Items)
	if pageSize > limit {
		pageSize = limit
	}
	items := make([]*dto.Chunk, 0, pageSize)
	for index := 0; index < pageSize; index++ {
		item := facts.Items[index]
		if item.Chunk == nil || item.ActiveRevision.Revision == nil {
			return nil, fmt.Errorf("Document Chunk facts item 不完整")
		}
		chunk := dto.ChunkFromModel(item.Chunk, item.ActiveRevision.Revision)
		chunk.ActiveRevision = chunkRevisionDTO(&item.ActiveRevision)
		items = append(items, chunk)
	}
	var nextCursor *string
	if len(facts.Items) > limit && pageSize > 0 {
		last := facts.Items[pageSize-1].Chunk
		encoded, err := encodeDocumentChunksCursor(documentChunksCursor{Sequence: last.Sequence, ID: last.ID})
		if err != nil {
			return nil, fmt.Errorf("编码 Document Chunk cursor 失败: %w", err)
		}
		nextCursor = &encoded
	}
	return &dto.DocumentChunkPage{
		GenerationID: facts.GenerationID, DocumentRevisionID: facts.DocumentRevisionID,
		ChunkSetID: facts.ChunkSetID, Items: items, NextCursor: nextCursor,
	}, nil
}

type documentChunksCursor struct {
	Sequence int       `json:"sequence"`
	ID       uuid.UUID `json:"id"`
}

func encodeDocumentChunksCursor(cursor documentChunksCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeDocumentChunksCursor(input string) (documentChunksCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(input))
	if err != nil {
		return documentChunksCursor{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor documentChunksCursor
	if err := decoder.Decode(&cursor); err != nil {
		return documentChunksCursor{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return documentChunksCursor{}, fmt.Errorf("cursor 包含多余内容")
	}
	if cursor.Sequence < 0 || cursor.ID == uuid.Nil {
		return documentChunksCursor{}, fmt.Errorf("cursor 字段无效")
	}
	return cursor, nil
}
