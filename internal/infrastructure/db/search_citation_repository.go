package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// SearchCitationRow 是 search_citations 表的 GORM Row 模型（v1.4.0 引用遥测）。
type SearchCitationRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null"`
	KbID            uuid.UUID `gorm:"type:uuid;not null"`
	SearchRunID     uuid.UUID `gorm:"type:uuid;not null"`
	DocumentID      uuid.UUID `gorm:"type:uuid;not null"`
	ChunkRevisionID uuid.UUID `gorm:"type:uuid;not null"`
	Rank            int       `gorm:"not null"`
	RetrievalStatus string    `gorm:"not null"`
	Transport       string    `gorm:"not null;default:''"`
	CreatedAt       time.Time
}

func (SearchCitationRow) TableName() string { return "search_citations" }

// SearchQueryLogRow 是 search_query_log 表的 GORM Row 模型（opt-in 查询原文）。
type SearchQueryLogRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null"`
	KbID            uuid.UUID `gorm:"type:uuid;not null"`
	SearchRunID     uuid.UUID `gorm:"type:uuid;not null"`
	QueryText       string
	QueryHash       string
	QueryChars      int
	RetrievalStatus string
	CreatedAt       time.Time
}

func (SearchQueryLogRow) TableName() string { return "search_query_log" }

func searchCitationToRow(entry model.SearchCitation) SearchCitationRow {
	return SearchCitationRow{
		ID: entry.ID, WorkspaceID: entry.WorkspaceID, KbID: entry.KbID,
		SearchRunID: entry.SearchRunID, DocumentID: entry.DocumentID,
		ChunkRevisionID: entry.ChunkRevisionID, Rank: entry.Rank,
		RetrievalStatus: string(entry.RetrievalStatus), Transport: entry.Transport,
		CreatedAt: entry.CreatedAt,
	}
}

func searchQueryLogToRow(entry model.SearchQueryLogEntry) SearchQueryLogRow {
	return SearchQueryLogRow{
		ID: entry.ID, WorkspaceID: entry.WorkspaceID, KbID: entry.KbID,
		SearchRunID: entry.SearchRunID, QueryText: entry.QueryText,
		QueryHash: entry.QueryHash, QueryChars: entry.QueryChars,
		RetrievalStatus: string(entry.RetrievalStatus), CreatedAt: entry.CreatedAt,
	}
}

// TelemetryRepository 持久化检索遥测（引用行 + opt-in 查询原文），读写显式带 workspace_id。
type TelemetryRepository struct {
	db *gorm.DB
}

// NewTelemetryRepository creates a telemetry repository.
func NewTelemetryRepository(db *gorm.DB) *TelemetryRepository {
	return &TelemetryRepository{db: db}
}

// InsertCitations 批量写入引用行；(search_run_id, chunk_revision_id) 冲突按
// DO NOTHING 跳过（重放检索是新 run 不冲突，此路径只兜投递重复）。
func (r *TelemetryRepository) InsertCitations(ctx context.Context, entries []model.SearchCitation) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([]SearchCitationRow, 0, len(entries))
	for i := range entries {
		if err := entries[i].Validate(); err != nil {
			return err
		}
		rows = append(rows, searchCitationToRow(entries[i]))
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
	if err != nil {
		return translateDBError(err, "批量写入引用遥测失败")
	}
	return nil
}

// InsertQueryLogEntries 批量写入查询原文行（投递侧已按 KB 开关门控，此处不再过滤）。
func (r *TelemetryRepository) InsertQueryLogEntries(ctx context.Context, entries []model.SearchQueryLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([]SearchQueryLogRow, 0, len(entries))
	for i := range entries {
		if err := entries[i].Validate(); err != nil {
			return err
		}
		rows = append(rows, searchQueryLogToRow(entries[i]))
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
	if err != nil {
		return translateDBError(err, "批量写入查询原文日志失败")
	}
	return nil
}

// GetQueryLogEnabled 读取 KB 的查询原文开关；跨 Workspace 或 KB 不存在返回 ErrNotFound。
// 单条主键点查（Take 感知未命中；Scan 无法区分 false 与不存在，不采用双查询）。
func (r *TelemetryRepository) GetQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID) (bool, error) {
	var row struct {
		QueryLogEnabled bool
	}
	err := r.db.WithContext(ctx).Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ? AND id = ?", workspaceID, kbID).
		Select("query_log_enabled").Take(&row).Error
	if err != nil {
		return false, translateDBError(err, "读取查询原文开关失败")
	}
	return row.QueryLogEnabled, nil
}

// GetQueryLogEnabledBatch 批量读取多个 KB 的查询原文开关（单条 IN 查询）；
// 不存在的 KB 不出现在结果里。多库检索投递用，避免逐库点查放大。
func (r *TelemetryRepository) GetQueryLogEnabledBatch(ctx context.Context, workspaceID uuid.UUID, kbIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	result := make(map[uuid.UUID]bool, len(kbIDs))
	if len(kbIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		ID              uuid.UUID
		QueryLogEnabled bool
	}
	err := r.db.WithContext(ctx).Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ? AND id IN ?", workspaceID, kbIDs).
		Select("id, query_log_enabled").Find(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "批量读取查询原文开关失败")
	}
	for _, row := range rows {
		result[row.ID] = row.QueryLogEnabled
	}
	return result, nil
}

// UpdateQueryLogEnabled 更新 KB 的查询原文开关。
func (r *TelemetryRepository) UpdateQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID, enabled bool) error {
	result := r.db.WithContext(ctx).Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ? AND id = ?", workspaceID, kbID).
		Update("query_log_enabled", enabled)
	if result.Error != nil {
		return translateDBError(result.Error, "更新查询原文开关失败")
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 知识库不存在", domainerrors.ErrNotFound)
	}
	return nil
}

// DeleteExpiredCitations 删除超过保留期的引用行（有界：子查询 LIMIT 双方言同构——
// GORM 的 DELETE 构建器不支持 LIMIT 子句，必须用 Raw SQL 保证批量上限）。
func (r *TelemetryRepository) DeleteExpiredCitations(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("%w: DeleteExpiredCitations limit 必须为正", domainerrors.ErrValidation)
	}
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM search_citations WHERE id IN (
			SELECT id FROM search_citations WHERE created_at < ? LIMIT ?
		)
	`, before, limit)
	if result.Error != nil {
		return 0, translateDBError(result.Error, "删除过期引用遥测失败")
	}
	return result.RowsAffected, nil
}

// DeleteExpiredQueryLogs 删除超过保留期的查询原文行（有界，同上）。
func (r *TelemetryRepository) DeleteExpiredQueryLogs(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("%w: DeleteExpiredQueryLogs limit 必须为正", domainerrors.ErrValidation)
	}
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM search_query_log WHERE id IN (
			SELECT id FROM search_query_log WHERE created_at < ? LIMIT ?
		)
	`, before, limit)
	if result.Error != nil {
		return 0, translateDBError(result.Error, "删除过期查询原文失败")
	}
	return result.RowsAffected, nil
}

var _ appservice.TelemetryStore = (*TelemetryRepository)(nil)
