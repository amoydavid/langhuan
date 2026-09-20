package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件实现 kb_sources 表的 CRUD 仓储与页面计数投影（v1.3.0 spec §3.2/§9.2）：
// 源行写入、按 lineage 读取、启停/改名/config 更新、软删、keep 删除分支的断链置 NULL、
// 以及按源聚合的页面/失败计数（documents.kb_source_id）。同步运行态读写
// （latch/cursor/last_result/源级 Job）见 kb_source_sync_store.go，两者共用 Row 与 codec。

// kbSourceRow 是 kb_sources 表的 GORM Row。
type KbSourceRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"column:kb_id;type:uuid;not null;index"`
	Kind               string
	Name               string
	Status             string
	Config             JSONMap `gorm:"type:jsonb"`
	SyncCursor         JSONMap `gorm:"type:jsonb"`
	NextSyncAt         *time.Time
	SyncRequestedForce bool
	SyncLastResult     JSONMap `gorm:"type:jsonb"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

func (KbSourceRow) TableName() string { return "kb_sources" }

func kbSourceToRow(source *model.KbSource) *KbSourceRow {
	return &KbSourceRow{
		ID: source.ID, WorkspaceID: source.WorkspaceID, KnowledgeBaseID: source.KnowledgeBaseID,
		Kind: string(source.Kind), Name: source.Name, Status: string(source.Status),
		Config: normalizedJSONMap(source.Config), SyncCursor: normalizedJSONMap(source.SyncCursor),
		NextSyncAt: source.NextSyncAt, SyncRequestedForce: source.SyncRequestedForce,
		SyncLastResult: normalizedJSONMap(source.SyncLastResult),
		CreatedAt:      source.CreatedAt, UpdatedAt: source.UpdatedAt, DeletedAt: source.DeletedAt,
	}
}

func kbSourceFromRow(row *KbSourceRow) *model.KbSource {
	return &model.KbSource{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		Kind: value.KbSourceKind(row.Kind), Name: row.Name, Status: value.KbSourceStatus(row.Status),
		Config: normalizedDomainMap(row.Config), SyncCursor: normalizedDomainMap(row.SyncCursor),
		NextSyncAt: row.NextSyncAt, SyncRequestedForce: row.SyncRequestedForce,
		SyncLastResult: normalizedDomainMap(row.SyncLastResult),
		CreatedAt:      row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt,
	}
}

// KbSourceDBRepository 是 kb_sources 表的薄封装：持久化与错误映射，不含业务规则。
type KbSourceDBRepository struct {
	db *gorm.DB
}

// NewKbSourceRepository 构造 kb_sources 仓储。
func NewKbSourceRepository(db *gorm.DB) *KbSourceDBRepository {
	return &KbSourceDBRepository{db: db}
}

// Create 写入一个内容源行。同库活源名称唯一（uq_kb_sources_active_name）冲突
// 经 translateDBError 映射为 ErrConflict（T1 评审移交：唯一名冲突的领域错误映射）。
func (r *KbSourceDBRepository) Create(ctx context.Context, source *model.KbSource) error {
	if err := r.db.WithContext(ctx).Create(kbSourceToRow(source)).Error; err != nil {
		return translateDBError(err, "创建内容源失败")
	}
	return nil
}

// Get 按 lineage 读取单个未软删内容源；不存在或已软删返回 ErrNotFound。
func (r *KbSourceDBRepository) Get(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (*model.KbSource, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, fmt.Errorf("%w: KbSource lineage 不能为空", domainerrors.ErrValidation)
	}
	var row KbSourceRow
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND kb_id = ? AND id = ?", workspaceID, kbID, sourceID).
		First(&row).Error
	if err != nil {
		return nil, translateDBError(err, "读取内容源失败")
	}
	// DeletedAt 是普通 *time.Time，GORM 不会自动过滤软删行，须显式拒绝。
	if row.DeletedAt != nil {
		return nil, domainerrors.ErrNotFound
	}
	return kbSourceFromRow(&row), nil
}

// List 按 (workspace, kb) 列出全部未软删内容源，按创建序稳定输出。
func (r *KbSourceDBRepository) List(ctx context.Context, workspaceID, kbID uuid.UUID) ([]*model.KbSource, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return nil, fmt.Errorf("%w: KbSource lineage 不能为空", domainerrors.ErrValidation)
	}
	var rows []KbSourceRow
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND kb_id = ? AND deleted_at IS NULL", workspaceID, kbID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "列出内容源失败")
	}
	result := make([]*model.KbSource, 0, len(rows))
	for i := range rows {
		result = append(result, kbSourceFromRow(&rows[i]))
	}
	return result, nil
}

// CountByKB 统计 KB 下未软删源数量（max_sources_per_kb 钳制用）。
func (r *KbSourceDBRepository) CountByKB(ctx context.Context, workspaceID, kbID uuid.UUID) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND kb_id = ? AND deleted_at IS NULL", workspaceID, kbID).
		Count(&count).Error
	if err != nil {
		return 0, translateDBError(err, "统计内容源数量失败")
	}
	return int(count), nil
}

// Update 只更新源的管理面可变列（name/status/config/updated_at），不触碰同步运行态
// （sync_cursor/next_sync_at/latch/last_result 可能被并发 worker 推进，禁止回写旧值）。
func (r *KbSourceDBRepository) Update(ctx context.Context, source *model.KbSource) error {
	result := r.db.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL",
			source.WorkspaceID, source.KnowledgeBaseID, source.ID).
		Updates(map[string]any{
			"name":       source.Name,
			"status":     string(source.Status),
			"config":     normalizedJSONMap(source.Config),
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return translateDBError(result.Error, "更新内容源失败")
	}
	if result.RowsAffected != 1 {
		return domainerrors.ErrNotFound
	}
	return nil
}

// SoftDelete 软删内容源行（仅未删行生效；已删/不存在返回 ErrNotFound）。
func (r *KbSourceDBRepository) SoftDelete(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, kbID, sourceID).
		Update("deleted_at", time.Now().UTC())
	if result.Error != nil {
		return translateDBError(result.Error, "删除内容源失败")
	}
	if result.RowsAffected != 1 {
		return domainerrors.ErrNotFound
	}
	return nil
}

// CountPagesBySource 聚合 KB 下每个源的页面数与失败页面数
// （走 documents.kb_source_id；无页面的源不出现在结果中）。SUM(CASE) 而非
// COUNT FILTER，保持 PG/SQLite 方言共用。
func (r *KbSourceDBRepository) CountPagesBySource(ctx context.Context, workspaceID, kbID uuid.UUID) (map[uuid.UUID]service.SourcePageCounts, error) {
	type pageCountsRow struct {
		KbSourceID uuid.UUID
		PageCount  int
		Failed     int
	}
	var rows []pageCountsRow
	err := r.db.WithContext(ctx).Table("documents").
		Select("kb_source_id, "+
			"SUM(CASE WHEN deleted_at IS NULL THEN 1 ELSE 0 END) AS page_count, "+
			"SUM(CASE WHEN deleted_at IS NULL AND status = ? THEN 1 ELSE 0 END) AS failed",
			string(value.DocumentStatusFailed)).
		Where("workspace_id = ? AND knowledge_base_id = ? AND kb_source_id IS NOT NULL", workspaceID, kbID).
		Group("kb_source_id").
		Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "统计内容源页面计数失败")
	}
	result := make(map[uuid.UUID]service.SourcePageCounts, len(rows))
	for _, row := range rows {
		result[row.KbSourceID] = service.SourcePageCounts{PageCount: row.PageCount, FailedPageCount: row.Failed}
	}
	return result, nil
}

// ListDocumentIDsBySource 列出源下全部未软删页面文档 ID（remove 删除分支的输入）。
func (r *KbSourceDBRepository) ListDocumentIDsBySource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) ([]uuid.UUID, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, fmt.Errorf("%w: KbSource lineage 不能为空", domainerrors.ErrValidation)
	}
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("documents").
		Where("workspace_id = ? AND knowledge_base_id = ? AND kb_source_id = ? AND deleted_at IS NULL",
			workspaceID, kbID, sourceID).
		Order("created_at ASC, id ASC").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, translateDBError(err, "列出内容源页面文档失败")
	}
	return ids, nil
}

// CountActiveBySource 统计源进行中的源级 source_sync 任务数（pending/running）。
// 管理面删除守卫用：有在飞任务时拒绝删除，避免 remove 分支软删源后新建页面
// 挂在已删源下残留。与调度侧 SourceSyncDBStore.CountActiveBySource 共享单一 SQL。
func (r *KbSourceDBRepository) CountActiveBySource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (int, error) {
	return countActiveSourceSyncJobs(ctx, r.db, workspaceID, kbID, sourceID)
}

// DetachSourceDocuments 把源的页面文档 kb_source_id 置 NULL（keep 删除分支：断链保页），
// 返回断链行数。已软删行一并断链，避免源重建后旧行被误归新源。
func (r *KbSourceDBRepository) DetachSourceDocuments(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (int64, error) {
	result := r.db.WithContext(ctx).Exec(
		"UPDATE documents SET kb_source_id = NULL, updated_at = ? "+
			"WHERE workspace_id = ? AND knowledge_base_id = ? AND kb_source_id = ?",
		time.Now().UTC(), workspaceID, kbID, sourceID,
	)
	if result.Error != nil {
		return 0, translateDBError(result.Error, "断开内容源页面关联失败")
	}
	return result.RowsAffected, nil
}

// 接口契约检查：本仓储实现 application 层定义的源存储端口（service/kb_source.go）。
var _ service.KbSourceStore = (*KbSourceDBRepository)(nil)
