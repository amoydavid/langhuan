package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// ChunkRevisionDBStore persists user Chunk revisions through Workspace transactions.
type ChunkRevisionDBStore struct{ db *gorm.DB }

// NewChunkRevisionStore creates a Chunk revision store.
func NewChunkRevisionStore(database *gorm.DB) *ChunkRevisionDBStore {
	return &ChunkRevisionDBStore{db: database}
}

// WithinWorkspace locks edit lineage inside a transaction-local tenant context.
func (s *ChunkRevisionDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.ChunkEditTx) error,
) error {
	if fn == nil {
		return fmt.Errorf("%w: ChunkEdit transaction callback 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &chunkRevisionDBTx{db: tx, workspaceID: workspaceID})
	})
}

// GetChunk reads a Chunk and its active revision through explicit Workspace/KB lineage.
func (s *ChunkRevisionDBStore) GetChunk(
	ctx context.Context,
	workspaceID, knowledgeBaseID, chunkID uuid.UUID,
) (*model.Chunk, *appservice.ChunkRevisionFacts, error) {
	var chunk *model.Chunk
	var revision *appservice.ChunkRevisionFacts
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var chunkRow ChunkRow
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND knowledge_base_id = ? AND id = ?", workspaceID, knowledgeBaseID, chunkID).
			First(&chunkRow).Error; err != nil {
			return translateDBError(err, "读取 Chunk 失败")
		}
		if chunkRow.ActiveRevisionID == nil {
			return fmt.Errorf("%w: Chunk 缺少 active Revision", domainerrors.ErrValidation)
		}
		facts, err := scanChunkRevisionFacts(ctx, chunkRevisionFactsQuery(tx).
			Where("chunk_revisions.workspace_id = ? AND chunk_revisions.chunk_id = ? AND chunk_revisions.id = ?", workspaceID, chunkID, *chunkRow.ActiveRevisionID))
		if err != nil {
			return err
		}
		if len(facts) != 1 {
			return domainerrors.ErrNotFound
		}
		var conversionErr error
		chunk, conversionErr = chunkV2FromRow(&chunkRow)
		if conversionErr != nil {
			return conversionErr
		}
		revision = facts[0]
		return nil
	})
	return chunk, revision, err
}

// ListChunkRevisions returns newest revisions first.
func (s *ChunkRevisionDBStore) ListChunkRevisions(
	ctx context.Context,
	workspaceID, knowledgeBaseID, chunkID uuid.UUID,
) ([]*appservice.ChunkRevisionFacts, error) {
	var result []*appservice.ChunkRevisionFacts
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var count int64
		if err := tx.WithContext(ctx).Model(&ChunkRow{}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND id = ?", workspaceID, knowledgeBaseID, chunkID).
			Count(&count).Error; err != nil {
			return translateDBError(err, "校验 Chunk lineage 失败")
		}
		if count != 1 {
			return domainerrors.ErrNotFound
		}
		var err error
		result, err = scanChunkRevisionFacts(ctx, chunkRevisionFactsQuery(tx).
			Where("chunk_revisions.workspace_id = ? AND chunk_revisions.knowledge_base_id = ? AND chunk_revisions.chunk_id = ?", workspaceID, knowledgeBaseID, chunkID).
			Order("chunk_revisions.revision_no DESC"))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type chunkRevisionDBTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *chunkRevisionDBTx) GetKnowledgeBaseForUpdate(ctx context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定 Chunk KnowledgeBase 失败")
	}
	return knowledgeBaseFromRow(&row)
}

func (tx *chunkRevisionDBTx) GetDocumentForUpdate(ctx context.Context, id uuid.UUID) (*model.Document, error) {
	var row DocumentRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定 Chunk Document 失败")
	}
	return documentV2FromRow(&row), nil
}

func (tx *chunkRevisionDBTx) GetChunkForUpdate(ctx context.Context, id uuid.UUID) (*model.Chunk, error) {
	var row ChunkRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定 Chunk 失败")
	}
	return chunkV2FromRow(&row)
}

func (tx *chunkRevisionDBTx) GetChunkRevision(ctx context.Context, id uuid.UUID) (*model.ChunkRevision, error) {
	var row ChunkRevisionRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取 ChunkRevision 失败")
	}
	return chunkRevisionFromRow(&row), nil
}

func (tx *chunkRevisionDBTx) NextChunkRevisionNo(ctx context.Context, chunkID uuid.UUID) (int64, error) {
	var next int64
	if err := tx.db.WithContext(ctx).Model(&ChunkRevisionRow{}).
		Select("COALESCE(MAX(revision_no), 0) + 1").
		Where("workspace_id = ? AND chunk_id = ?", tx.workspaceID, chunkID).
		Scan(&next).Error; err != nil {
		return 0, translateDBError(err, "分配 ChunkRevision revision_no 失败")
	}
	if next < 1 {
		return 0, fmt.Errorf("%w: ChunkRevision revision_no 无效", domainerrors.ErrValidation)
	}
	return next, nil
}

func (tx *chunkRevisionDBTx) CreateChunkRevisionAndJob(
	ctx context.Context,
	revision *model.ChunkRevision,
	job *model.Job,
) error {
	if revision == nil || job == nil || revision.WorkspaceID != tx.workspaceID || job.WorkspaceID != tx.workspaceID ||
		revision.KnowledgeBaseID != job.KnowledgeBaseID || revision.DocumentID != job.DocumentID ||
		revision.DocumentRevisionID != job.DocumentRevisionID {
		return fmt.Errorf("%w: ChunkRevision/Job lineage 不一致", domainerrors.ErrValidation)
	}
	if err := tx.db.WithContext(ctx).Create(chunkRevisionToRow(revision)).Error; err != nil {
		return translateDBError(err, "创建 user ChunkRevision 失败")
	}
	if err := tx.db.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		return translateDBError(err, "创建 ChunkRevision Job 失败")
	}
	return nil
}
