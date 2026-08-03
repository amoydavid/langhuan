package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

const (
	chunkRevisionIndexJobType = "chunk_revision_index"
	maxChunkRevisionRunes     = 100_000
)

// ChunkRevisionStore owns Chunk reads and optimistic revision creation.
type ChunkRevisionStore interface {
	ChunkEditStore
	GetChunk(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*model.Chunk, *ChunkRevisionFacts, error)
	ListChunkRevisions(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]*ChunkRevisionFacts, error)
}

// ChunkRevisionFacts adds the optional current nickname to an immutable revision.
type ChunkRevisionFacts struct {
	Revision       *model.ChunkRevision
	EditorNickname *string
}

// CreateChunkRevisionInput is the complete optimistic edit request.
type CreateChunkRevisionInput struct {
	WorkspaceID, KnowledgeBaseID, ChunkID uuid.UUID
	BaseRevisionID                        uuid.UUID
	Content, ContextHeader                string
	Enabled                               bool
	EditorUserID                          uuid.UUID
	ActorRole                             value.WorkspaceRole
}

// ChunkRevisionService manages immutable user Chunk revisions.
type ChunkRevisionService struct {
	store ChunkRevisionStore
	queue queue.JobQueue
}

// NewChunkRevisionService creates the Chunk revision use case.
func NewChunkRevisionService(store ChunkRevisionStore, jobQueue queue.JobQueue) *ChunkRevisionService {
	return &ChunkRevisionService{store: store, queue: jobQueue}
}

// Get returns one Chunk and its active revision inside the requested KB lineage.
func (s *ChunkRevisionService) Get(ctx context.Context, workspaceID, knowledgeBaseID, chunkID uuid.UUID) (*dto.Chunk, error) {
	chunk, revision, err := s.store.GetChunk(ctx, workspaceID, knowledgeBaseID, chunkID)
	if err != nil {
		return nil, err
	}
	result := dto.ChunkFromModel(chunk, revision.Revision)
	result.ActiveRevision = chunkRevisionDTO(revision)
	return result, nil
}

// List returns immutable revisions ordered newest first.
func (s *ChunkRevisionService) List(ctx context.Context, workspaceID, knowledgeBaseID, chunkID uuid.UUID) ([]*dto.ChunkRevision, error) {
	revisions, err := s.store.ListChunkRevisions(ctx, workspaceID, knowledgeBaseID, chunkID)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.ChunkRevision, len(revisions))
	for index, revision := range revisions {
		result[index] = chunkRevisionDTO(revision)
	}
	return result, nil
}

func chunkRevisionDTO(facts *ChunkRevisionFacts) *dto.ChunkRevision {
	if facts == nil || facts.Revision == nil {
		return nil
	}
	result := dto.ChunkRevisionFromModel(facts.Revision)
	switch facts.Revision.EditSource {
	case value.ChunkEditSourceSystem:
		result.EditorDisplayName = "系统"
	case value.ChunkEditSourceUser:
		if facts.EditorNickname != nil && strings.TrimSpace(*facts.EditorNickname) != "" {
			result.EditorDisplayName = strings.TrimSpace(*facts.EditorNickname)
		} else {
			result.EditorDisplayName = "已删除用户"
		}
	}
	return result
}

// Create appends one user revision under an optimistic base pointer and enqueues targeted indexing.
func (s *ChunkRevisionService) Create(ctx context.Context, input CreateChunkRevisionInput) (*dto.ChunkRevision, error) {
	if err := validateCreateChunkRevisionInput(input); err != nil {
		return nil, err
	}
	if s.store == nil || s.queue == nil {
		return nil, fmt.Errorf("%w: Chunk Revision store/queue 不能为空", domainerrors.ErrValidation)
	}
	var revision *model.ChunkRevision
	var job *model.Job
	var generationID uuid.UUID
	var expectedContentVersion int64
	err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx ChunkEditTx) error {
		knowledgeBase, err := tx.GetKnowledgeBaseForUpdate(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if knowledgeBase.ActiveIndexGenerationID == nil || *knowledgeBase.ActiveIndexGenerationID == uuid.Nil {
			return fmt.Errorf("%w: KnowledgeBase 缺少 active Generation", domainerrors.ErrValidation)
		}
		chunk, err := tx.GetChunkForUpdate(txCtx, input.ChunkID)
		if err != nil {
			return err
		}
		if chunk.WorkspaceID != input.WorkspaceID || chunk.KnowledgeBaseID != input.KnowledgeBaseID {
			return domainerrors.ErrNotFound
		}
		document, err := tx.GetDocumentForUpdate(txCtx, chunk.DocumentID)
		if err != nil {
			return err
		}
		if document.WorkspaceID != input.WorkspaceID || document.KnowledgeBaseID != input.KnowledgeBaseID {
			return domainerrors.ErrNotFound
		}
		if document.Kind == value.DocumentKindFAQ {
			return domainerrors.ErrFAQChunkImmutable
		}
		if chunk.ActiveRevisionID == nil || *chunk.ActiveRevisionID != input.BaseRevisionID {
			return domainerrors.ErrRevisionConflict
		}
		base, err := tx.GetChunkRevision(txCtx, input.BaseRevisionID)
		if err != nil {
			return err
		}
		if base.ChunkID != chunk.ID || base.WorkspaceID != input.WorkspaceID {
			return domainerrors.ErrRevisionConflict
		}
		nextRevisionNo, err := tx.NextChunkRevisionNo(txCtx, chunk.ID)
		if err != nil {
			return err
		}
		generationID = *knowledgeBase.ActiveIndexGenerationID
		expectedContentVersion = knowledgeBase.ContentVersion
		editorID, baseID := input.EditorUserID, input.BaseRevisionID
		revision, err = model.NewChunkRevision(model.NewChunkRevisionInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			DocumentID: chunk.DocumentID, DocumentRevisionID: chunk.DocumentRevisionID,
			ChunkSetID: chunk.ChunkSetID, ChunkID: chunk.ID, RevisionNo: nextRevisionNo,
			BaseRevisionID: &baseID, Content: input.Content, ContextHeader: input.ContextHeader,
			EmbeddingContent: chunkRevisionEmbeddingContent(input.ContextHeader, input.Content),
			Enabled:          input.Enabled, Status: value.ChunkRevisionPending,
			EditSource: value.ChunkEditSourceUser, EditorUserID: &editorID,
		})
		if err != nil {
			return err
		}
		job, err = model.NewJob(model.NewJobInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			DocumentID: chunk.DocumentID, DocumentRevisionID: chunk.DocumentRevisionID,
			Type: chunkRevisionIndexJobType, Status: value.JobStatusPending,
			Payload: map[string]any{
				"workspace_id": input.WorkspaceID.String(), "knowledge_base_id": input.KnowledgeBaseID.String(),
				"document_id": chunk.DocumentID.String(), "document_revision_id": chunk.DocumentRevisionID.String(),
				"chunk_set_id": chunk.ChunkSetID.String(), "chunk_id": chunk.ID.String(),
				"base_revision_id": input.BaseRevisionID.String(), "new_revision_id": revision.ID.String(),
				"index_generation_id": generationID.String(), "expected_content_version": knowledgeBase.ContentVersion,
			},
		})
		if err != nil {
			return err
		}
		return tx.CreateChunkRevisionAndJob(txCtx, revision, job)
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"workspace_id": input.WorkspaceID, "knowledge_base_id": input.KnowledgeBaseID,
		"document_id": revision.DocumentID, "document_revision_id": revision.DocumentRevisionID,
		"chunk_set_id": revision.ChunkSetID, "chunk_id": revision.ChunkID,
		"base_revision_id": input.BaseRevisionID, "new_revision_id": revision.ID,
		"generation_id": generationID, "expected_content_version": expectedContentVersion, "job_id": job.ID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: chunkRevisionIndexJobType, Payload: payload,
		TaskID: queue.DocumentTaskID(chunkRevisionIndexJobType, input.WorkspaceID, revision.ID, generationID),
	}); err != nil {
		return nil, fmt.Errorf("入队 ChunkRevision 索引任务失败: %w", err)
	}
	return dto.ChunkRevisionFromModel(revision), nil
}

func validateCreateChunkRevisionInput(input CreateChunkRevisionInput) error {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.ChunkID == uuid.Nil ||
		input.BaseRevisionID == uuid.Nil || input.EditorUserID == uuid.Nil {
		return fmt.Errorf("%w: Chunk Revision lineage 不能为空", domainerrors.ErrValidation)
	}
	if !input.ActorRole.AtLeast(value.RoleAdmin) {
		return domainerrors.ErrForbidden
	}
	if !utf8.ValidString(input.Content) || !utf8.ValidString(input.ContextHeader) ||
		utf8.RuneCountInString(input.Content)+utf8.RuneCountInString(input.ContextHeader) > maxChunkRevisionRunes {
		return fmt.Errorf("%w: Chunk Revision 必须是有效 UTF-8 且不超过 %d 字符", domainerrors.ErrValidation, maxChunkRevisionRunes)
	}
	if input.Enabled && strings.TrimSpace(input.Content) == "" {
		return fmt.Errorf("%w: 启用的 Chunk Revision content 不能为空", domainerrors.ErrValidation)
	}
	return nil
}

func chunkRevisionEmbeddingContent(header, content string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return content
	}
	return header + "\n\n" + content
}
