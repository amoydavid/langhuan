package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentPublishDBStore atomically publishes one staged document projection.
type DocumentPublishDBStore struct {
	db *gorm.DB
}

// publishEntryBatchSize 分批发布 retrieval_entries 的批大小。单文档 chunk 可能
// 很多，一次性 `id IN (...)` 会产生超大 SQL 报文并锁住整文档所有行；分批保持
// 同一事务，逐批累加校验，语义与一次性更新完全一致。
const publishEntryBatchSize = 1000

// uuidStrings 把 uuid 列表转成字符串切片，配合 pq.Array 以单个数组参数绑定
// `id = ANY(?::uuid[])`：pq.Array 作为 driver.Valuer 返回数组字面量（GORM 不会
// 展开 slice），经 SQL cast 为 uuid[] 参与相等比较，与 `id IN (...)` 完全等价
// 但只占一个绑定参数，规避 PostgreSQL 参数上限。
func uuidStrings(ids []uuid.UUID) []string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = id.String()
	}
	return values
}

// NewDocumentPublishDBStore creates a document publication store.
func NewDocumentPublishDBStore(database *gorm.DB) *DocumentPublishDBStore {
	return &DocumentPublishDBStore{db: database}
}

// WithinWorkspace runs publication through one tenant-local transaction.
func (s *DocumentPublishDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.DocumentPublishTx) error,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &documentPublishTx{db: tx, workspaceID: workspaceID})
	})
}

type documentPublishTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *documentPublishTx) GetDocumentForUpdate(ctx context.Context, id uuid.UUID) (*model.Document, error) {
	var row DocumentRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定待发布 Document 失败")
	}
	return documentV2FromRow(&row), nil
}

func (tx *documentPublishTx) GetKnowledgeBaseForUpdate(ctx context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定待发布 KnowledgeBase 失败")
	}
	return knowledgeBaseV2FromRow(&row), nil
}

func (tx *documentPublishTx) PublishDocument(
	ctx context.Context,
	document *model.Document,
	chunkSet *model.DocumentChunkSet,
	chunks []*model.Chunk,
	revisions []*model.ChunkRevision,
	entries []*model.RetrievalEntry,
) error {
	generationID, err := validateDocumentPublication(
		tx.workspaceID, document, chunkSet, chunks, revisions, entries,
	)
	if err != nil {
		return err
	}
	var storedDocument DocumentRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, document.ID).
		First(&storedDocument).Error; err != nil {
		return translateDBError(err, "锁定发布 Document 失败")
	}
	if storedDocument.KnowledgeBaseID != chunkSet.KnowledgeBaseID || storedDocument.Kind != string(document.Kind) {
		return domainerrors.ErrNotFound
	}
	var revisionRow DocumentRevisionRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			"workspace_id = ? AND knowledge_base_id = ? AND document_id = ? AND id = ? AND status = ?",
			tx.workspaceID, chunkSet.KnowledgeBaseID, document.ID,
			chunkSet.DocumentRevisionID, value.DocumentRevisionReady,
		).First(&revisionRow).Error; err != nil {
		return translateDBError(err, "锁定发布 DocumentRevision 失败")
	}
	var knowledgeBaseRow KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, chunkSet.KnowledgeBaseID).
		First(&knowledgeBaseRow).Error; err != nil {
		return translateDBError(err, "锁定发布 KnowledgeBase 失败")
	}
	if knowledgeBaseRow.ActiveIndexGenerationID == nil {
		return fmt.Errorf("%w: KnowledgeBase 缺少 active Generation", domainerrors.ErrValidation)
	}
	if generationID == uuid.Nil {
		generationID = *knowledgeBaseRow.ActiveIndexGenerationID
	}
	if *knowledgeBaseRow.ActiveIndexGenerationID != generationID {
		return domainerrors.ErrGenerationStale
	}
	var generationRow IndexGenerationRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND status IN ?",
			tx.workspaceID, chunkSet.KnowledgeBaseID, generationID,
			[]string{string(value.IndexGenerationReady), string(value.IndexGenerationBuilding)},
		).First(&generationRow).Error; err != nil {
		return translateDBError(err, "锁定发布 IndexGeneration 失败")
	}
	if generationRow.IndexedContentVersion != knowledgeBaseRow.ContentVersion {
		return domainerrors.ErrGenerationStale
	}
	if err := tx.requireCompleteStaging(ctx, generationID, chunks, revisions, entries); err != nil {
		return err
	}

	now := time.Now().UTC()
	if err := tx.db.WithContext(ctx).Model(&RetrievalEntryRow{}).
		Where(
			"workspace_id = ? AND index_generation_id = ? AND document_id = ? AND state = ?",
			tx.workspaceID, generationID, document.ID, value.RetrievalEntryPublished,
		).Updates(map[string]any{
		"state": string(value.RetrievalEntryRetired), "retired_at": now,
	}).Error; err != nil {
		return translateDBError(err, "退役旧 RetrievalEntries 失败")
	}
	entryIDs := make([]uuid.UUID, len(entries))
	for index, entry := range entries {
		entryIDs[index] = entry.ID
	}
	if len(entryIDs) > 0 {
		var published int64
		for start := 0; start < len(entryIDs); start += publishEntryBatchSize {
			end := min(start+publishEntryBatchSize, len(entryIDs))
			result := tx.db.WithContext(ctx).Model(&RetrievalEntryRow{}).
				Where("workspace_id = ? AND id = ANY(?::uuid[]) AND state = ?", tx.workspaceID, pq.Array(uuidStrings(entryIDs[start:end])), value.RetrievalEntryStaging).
				Updates(map[string]any{
					"state": string(value.RetrievalEntryPublished), "published_at": now, "retired_at": nil,
				})
			if result.Error != nil {
				return translateDBError(result.Error, "发布 RetrievalEntries 失败")
			}
			published += result.RowsAffected
		}
		if published != int64(len(entryIDs)) {
			return fmt.Errorf("%w: RetrievalEntry staging 数量变化", domainerrors.ErrConflict)
		}
	}
	for index, chunk := range chunks {
		revision := revisions[index]
		result := tx.db.WithContext(ctx).Model(&ChunkRow{}).
			Where("workspace_id = ? AND id = ? AND chunk_set_id = ?", tx.workspaceID, chunk.ID, chunkSet.ID).
			Update("active_revision_id", revision.ID)
		if result.Error != nil {
			return translateDBError(result.Error, "切换 Chunk active revision 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		updates := map[string]any{"status": string(value.ChunkRevisionReady)}
		if revision.Enabled {
			updates["indexed_at"] = now
		}
		if err := tx.db.WithContext(ctx).Model(&ChunkRevisionRow{}).
			Where("workspace_id = ? AND id = ?", tx.workspaceID, revision.ID).
			Updates(updates).Error; err != nil {
			return translateDBError(err, "完成 ChunkRevision 发布失败")
		}
	}
	if err := tx.db.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, document.ID).
		Updates(map[string]any{
			"active_revision_id": chunkSet.DocumentRevisionID,
			"status":             string(value.DocumentStatusReady), "updated_at": now,
		}).Error; err != nil {
		return translateDBError(err, "切换 Document active revision 失败")
	}
	contentVersion := knowledgeBaseRow.ContentVersion + 1
	kbResult := tx.db.WithContext(ctx).Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ? AND id = ? AND content_version = ?", tx.workspaceID, knowledgeBaseRow.ID, knowledgeBaseRow.ContentVersion).
		Updates(map[string]any{"content_version": contentVersion, "updated_at": now})
	if kbResult.Error != nil {
		return translateDBError(kbResult.Error, "推进 KnowledgeBase content version 失败")
	}
	if kbResult.RowsAffected != 1 {
		return domainerrors.ErrGenerationStale
	}
	stats, err := loadIndexGenerationProjectionStats(
		ctx, tx.db, tx.workspaceID, knowledgeBaseRow.ID, generationID,
	)
	if err != nil {
		return err
	}
	result := tx.db.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where(
			"workspace_id = ? AND id = ? AND indexed_content_version = ?",
			tx.workspaceID, generationID, generationRow.IndexedContentVersion,
		).Updates(map[string]any{
		"indexed_content_version": contentVersion,
		"document_count":          stats.DocumentCount,
		"chunk_count":             stats.ChunkCount,
		"indexed_count":           stats.IndexedCount,
		"manual_edit_count":       stats.ManualEditCount,
		"disabled_chunk_count":    stats.DisabledChunkCount,
	})
	if result.Error != nil {
		return translateDBError(result.Error, "推进 Generation indexed content version 失败")
	}
	if result.RowsAffected != 1 {
		return domainerrors.ErrGenerationStale
	}
	return nil
}

func (tx *documentPublishTx) requireCompleteStaging(
	ctx context.Context,
	generationID uuid.UUID,
	chunks []*model.Chunk,
	revisions []*model.ChunkRevision,
	entries []*model.RetrievalEntry,
) error {
	expected := make(map[uuid.UUID]uuid.UUID)
	for index, revision := range revisions {
		role := chunks[index].Role
		if role == "" {
			role = value.ChunkRoleFlat
		}
		if revision.Enabled && role.IsRetrievable() {
			expected[revision.ChunkID] = revision.ID
		}
	}
	if len(expected) != len(entries) {
		return fmt.Errorf("%w: enabled ChunkRevision 与 staging 数量不一致", domainerrors.ErrValidation)
	}
	entryIDs := make([]uuid.UUID, len(entries))
	for index, entry := range entries {
		if expected[entry.ChunkID] != entry.ChunkRevisionID || entry.IndexGenerationID != generationID {
			return fmt.Errorf("%w: staging entry 与 active ChunkRevision 不一致", domainerrors.ErrValidation)
		}
		entryIDs[index] = entry.ID
	}
	if len(entryIDs) == 0 {
		return nil
	}
	var count int64
	for start := 0; start < len(entryIDs); start += publishEntryBatchSize {
		end := min(start+publishEntryBatchSize, len(entryIDs))
		var batchCount int64
		if err := tx.db.WithContext(ctx).Model(&RetrievalEntryRow{}).
			Where(
				"workspace_id = ? AND index_generation_id = ? AND id = ANY(?::uuid[]) AND state = ? "+
					"AND embedding IS NOT NULL AND dimension IS NOT NULL AND fts_document IS NOT NULL",
				tx.workspaceID, generationID, pq.Array(uuidStrings(entryIDs[start:end])), value.RetrievalEntryStaging,
			).Count(&batchCount).Error; err != nil {
			return translateDBError(err, "校验 RetrievalEntry staging 完整性失败")
		}
		count += batchCount
	}
	if count != int64(len(entryIDs)) {
		return fmt.Errorf("%w: RetrievalEntry staging 不完整", domainerrors.ErrConflict)
	}
	return nil
}

func validateDocumentPublication(
	workspaceID uuid.UUID,
	document *model.Document,
	chunkSet *model.DocumentChunkSet,
	chunks []*model.Chunk,
	revisions []*model.ChunkRevision,
	entries []*model.RetrievalEntry,
) (uuid.UUID, error) {
	if document == nil || chunkSet == nil || workspaceID == uuid.Nil ||
		document.WorkspaceID != workspaceID || chunkSet.WorkspaceID != workspaceID ||
		document.ID != chunkSet.DocumentID || document.KnowledgeBaseID != chunkSet.KnowledgeBaseID ||
		chunkSet.Status != value.ChunkSetReady || document.ActiveRevisionID == nil ||
		*document.ActiveRevisionID != chunkSet.DocumentRevisionID || len(chunks) != len(revisions) ||
		int64(len(chunks)) != chunkSet.ChunkCount {
		return uuid.Nil, fmt.Errorf("%w: Document publication aggregate 无效", domainerrors.ErrValidation)
	}
	for index, chunk := range chunks {
		revision := revisions[index]
		if chunk == nil || revision == nil ||
			chunk.WorkspaceID != workspaceID || chunk.ChunkSetID != chunkSet.ID ||
			revision.WorkspaceID != workspaceID || revision.ChunkID != chunk.ID ||
			revision.ChunkSetID != chunkSet.ID {
			return uuid.Nil, fmt.Errorf("%w: Document publication chunk %d 无效", domainerrors.ErrValidation, index)
		}
		role := chunk.Role
		if role == "" {
			role = value.ChunkRoleFlat
		}
		if err := role.Validate(); err != nil || (role == value.ChunkRoleChild && chunk.ParentChunkID == nil) {
			return uuid.Nil, fmt.Errorf("%w: Document publication chunk %d role 无效", domainerrors.ErrValidation, index)
		}
	}
	chunksByID := make(map[uuid.UUID]*model.Chunk, len(chunks))
	for _, chunk := range chunks {
		chunksByID[chunk.ID] = chunk
	}
	generationID := uuid.Nil
	for _, entry := range entries {
		if entry == nil || entry.WorkspaceID != workspaceID || entry.KnowledgeBaseID != chunkSet.KnowledgeBaseID ||
			entry.DocumentID != document.ID || entry.DocumentRevisionID != chunkSet.DocumentRevisionID ||
			entry.ChunkSetID != chunkSet.ID || entry.State != value.RetrievalEntryStaging {
			return uuid.Nil, fmt.Errorf("%w: Document publication entry lineage 无效", domainerrors.ErrValidation)
		}
		chunk, ok := chunksByID[entry.ChunkID]
		if !ok {
			return uuid.Nil, fmt.Errorf("%w: Document publication entry 指向未知 Chunk", domainerrors.ErrValidation)
		}
		role := chunk.Role
		if role == "" {
			role = value.ChunkRoleFlat
		}
		if !role.IsRetrievable() {
			return uuid.Nil, fmt.Errorf("%w: Document publication entry 不能指向 parent Chunk", domainerrors.ErrValidation)
		}
		if generationID == uuid.Nil {
			generationID = entry.IndexGenerationID
		} else if generationID != entry.IndexGenerationID {
			return uuid.Nil, fmt.Errorf("%w: entries 跨 Generation", domainerrors.ErrValidation)
		}
	}
	return generationID, nil
}
