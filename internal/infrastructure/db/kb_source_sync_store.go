package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件实现 web 内容源（kb_sources 行）同步执行路径的运行态读写
// （v1.3.0 spec §3.2/§8）：读取源行、源级 force latch 消费、源级 source_sync Job
// 终态化、sync_cursor/sync_last_result 推进与 documents.source_meta 写回。
// 全部方法挂在 SourceSyncDBStore 上，与飞书 KB 级方法（source_sync_store.go）并列；
// Meta Scheduler 的 web 源调度读写见 kb_source_sync_store_web.go。
// 事务边界沿用 WorkspaceTxRunner（kb_sources 复合外键携带 workspace lineage）。
// kb_sources 的 GORM Row 与 toRow/fromRow codec 在 kb_source_repository.go（CRUD 仓储），
// sync_cursor jsonb 与飞书 KB 的 source_config 运行态同形：键 "sync_cursor" 存 RFC3339。

// GetSyncKbSource 读取一个内容源（含 config 与运行态列）；已软删或不存在返回 ErrNotFound。
func (s *SourceSyncDBStore) GetSyncKbSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (*model.KbSource, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, fmt.Errorf("%w: GetSyncKbSource lineage 不能为空", domainerrors.ErrValidation)
	}
	var row KbSourceRow
	err := s.db.WithContext(ctx).
		Where("workspace_id = ? AND kb_id = ? AND id = ?", workspaceID, kbID, sourceID).
		First(&row).Error
	if err != nil {
		return nil, translateDBError(err, "读取内容源失败")
	}
	// DeletedAt 是普通 *time.Time，GORM 不会自动过滤软删行，须显式拒绝
	//（与 lockKbSourceForUpdate 同口径）。
	if row.DeletedAt != nil {
		return nil, domainerrors.ErrNotFound
	}
	return kbSourceFromRow(&row), nil
}

// ConsumeSourceForceLatch 原子读取并清空源级 force latch（kb_sources.sync_requested_force）。
// jobID 用于 lineage 校验，语义与 KB 级 ConsumeForceLatch 一致。
func (s *SourceSyncDBStore) ConsumeSourceForceLatch(ctx context.Context, workspaceID, kbID, sourceID, jobID uuid.UUID) (bool, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil || jobID == uuid.Nil {
		return false, fmt.Errorf("%w: ConsumeSourceForceLatch lineage/job 不能为空", domainerrors.ErrValidation)
	}
	var consumed bool
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		row, err := lockKbSourceForUpdate(ctx, tx, workspaceID, kbID, sourceID)
		if err != nil {
			return err
		}
		consumed = row.SyncRequestedForce
		return setKbSourceForceLatchTx(ctx, tx, workspaceID, kbID, sourceID, false, time.Now().UTC())
	})
	if err != nil {
		return false, err
	}
	return consumed, nil
}

// FinalizeSourceSyncJobForSource 在源行锁定事务内：标记源级 source_sync Job 终态
// （按 kb_source_id 定位，幂等：仅 pending/running 可转换），读取源级 latch；
// 为 true 则清空 latch 并新建下一个源级 source_sync Job 返回（调用方入队）。
func (s *SourceSyncDBStore) FinalizeSourceSyncJobForSource(
	ctx context.Context, workspaceID, kbID, sourceID, jobID uuid.UUID, status value.JobStatus, errorMessage string,
) (*model.Job, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil || jobID == uuid.Nil {
		return nil, fmt.Errorf("%w: FinalizeSourceSyncJobForSource lineage/job 不能为空", domainerrors.ErrValidation)
	}
	if status == "" {
		return nil, fmt.Errorf("%w: FinalizeSourceSyncJobForSource status 不能为空", domainerrors.ErrValidation)
	}
	var next *model.Job
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		// 先锁定源行（保证 latch 检查与 Job 终态化在同一事务）。
		if _, err := lockKbSourceForUpdate(ctx, tx, workspaceID, kbID, sourceID); err != nil {
			return err
		}

		now := time.Now().UTC()
		updates := map[string]any{
			"status":     string(status),
			"updated_at": now,
		}
		if errorMessage != "" {
			updates["error_message"] = errorMessage
		}
		result := tx.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND kb_source_id = ? AND id = ? AND type = ? AND status IN ?",
				workspaceID, kbID, sourceID, jobID, model.SourceSyncJobType,
				[]string{string(value.JobStatusPending), string(value.JobStatusRunning)}).
			Updates(updates)
		if result.Error != nil {
			return translateDBError(result.Error, "标记源级 source_sync Job 终态失败")
		}
		if result.RowsAffected == 0 {
			// Job 已是终态或不存在：幂等跳过，不再创建后续 Job（与 KB 级语义一致）。
			next = nil
			return nil
		}

		// 读取源级 latch（源行仍被本事务锁定）。
		row, err := lockKbSourceForUpdate(ctx, tx, workspaceID, kbID, sourceID)
		if err != nil {
			return err
		}
		if !row.SyncRequestedForce {
			return nil
		}
		if err := setKbSourceForceLatchTx(ctx, tx, workspaceID, kbID, sourceID, false, now); err != nil {
			return err
		}
		newJob, err := model.NewJob(model.NewJobInput{
			WorkspaceID: workspaceID, KnowledgeBaseID: kbID, KbSourceID: sourceID,
			// 标注源引用的连接（与 Meta Scheduler 派发的 Job 同形），
			// 供 CountActiveByConnection 的 web 源 connection 组限流统计。
			SourceConnectionID: sourceConnectionIDFromConfig(row.Config),
			Type:               model.SourceSyncJobType, Status: value.JobStatusPending,
		})
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(jobV2ToRow(newJob)).Error; err != nil {
			return translateDBError(err, "创建下一个源级 source_sync Job 失败")
		}
		next = newJob
		return nil
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// UpdateSourceSyncCursor 把安全 watermark 写入 kb_sources.sync_cursor（键 "sync_cursor"，
// RFC3339，与飞书 KB source_config 运行态同形）；cursor 为零值时清除该键。
func (s *SourceSyncDBStore) UpdateSourceSyncCursor(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, cursor time.Time) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return fmt.Errorf("%w: UpdateSourceSyncCursor lineage 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var execSQL string
		var args []any
		if tx.Dialector.Name() == "sqlite" {
			execSQL = "UPDATE kb_sources SET sync_cursor = json_set(sync_cursor, '$.sync_cursor', ?), updated_at = ? WHERE workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{cursor.UTC().Format(time.RFC3339Nano), now, workspaceID, kbID, sourceID}
		} else {
			execSQL = "UPDATE kb_sources SET sync_cursor = jsonb_set(sync_cursor, '{sync_cursor}', to_jsonb(?::timestamptz)), updated_at = ? WHERE workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{cursor.UTC(), now, workspaceID, kbID, sourceID}
		}
		result := tx.WithContext(ctx).Exec(execSQL, args...)
		if result.Error != nil {
			return translateDBError(result.Error, "更新内容源 sync_cursor 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// UpdateSourceSyncResult 把 SyncResult 整列写入 kb_sources.sync_last_result
// （列语义即"上次结果摘要"，无需 jsonb_set 保留其它键）。
func (s *SourceSyncDBStore) UpdateSourceSyncResult(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, result service.SyncResult) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return fmt.Errorf("%w: UpdateSourceSyncResult lineage 不能为空", domainerrors.ErrValidation)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("编码源同步 SyncResult 失败: %w", err)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var execSQL string
		if tx.Dialector.Name() == "sqlite" {
			execSQL = "UPDATE kb_sources SET sync_last_result = json(?), updated_at = ? WHERE workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL"
		} else {
			execSQL = "UPDATE kb_sources SET sync_last_result = ?::jsonb, updated_at = ? WHERE workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL"
		}
		res := tx.WithContext(ctx).Exec(execSQL, string(payload), now, workspaceID, kbID, sourceID)
		if res.Error != nil {
			return translateDBError(res.Error, "更新内容源 sync_last_result 失败")
		}
		if res.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// UpdateDocumentSourceMeta 原样替换 documents.source_meta（不建 revision）。
// 304 unchanged 路径用它刷新 last_fetch_at 等观测值；「旧值合并本次结果」的合并
// 语义由调用方完成（SourceMeta.ToMap 零值省略，见 ports/source 注释）。
func (s *SourceSyncDBStore) UpdateDocumentSourceMeta(ctx context.Context, workspaceID, documentID uuid.UUID, sourceMeta map[string]any) error {
	if workspaceID == uuid.Nil || documentID == uuid.Nil {
		return fmt.Errorf("%w: UpdateDocumentSourceMeta lineage 不能为空", domainerrors.ErrValidation)
	}
	if sourceMeta == nil {
		sourceMeta = map[string]any{}
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, documentID).
			Updates(map[string]any{
				"source_meta": normalizedJSONMap(sourceMeta),
				"updated_at":  time.Now().UTC(),
			})
		if result.Error != nil {
			return translateDBError(result.Error, "更新文档 source_meta 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// sourceConnectionIDFromConfig 从 kb_sources.config 提取 fetch.connection_id
// （缺失/非法按"无连接"零值处理）；db 层复用 application 层的解析语义保持同形。
func sourceConnectionIDFromConfig(config JSONMap) uuid.UUID {
	connectionID, _ := service.WebFetchConnectionID(normalizedDomainMap(config))
	return connectionID
}

// lockKbSourceForUpdate 锁定 kb_sources 行（FOR UPDATE）。未找到（含已软删——软删行
// 也会被 First 命中，需显式拒绝）返回 ErrNotFound。
func lockKbSourceForUpdate(ctx context.Context, tx *gorm.DB, workspaceID, kbID, sourceID uuid.UUID) (*KbSourceRow, error) {
	var row KbSourceRow
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND kb_id = ? AND id = ?", workspaceID, kbID, sourceID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, translateDBError(err, "锁定内容源失败")
	}
	if row.DeletedAt != nil {
		return nil, domainerrors.ErrNotFound
	}
	return &row, nil
}

// setKbSourceForceLatchTx 写入源级 force latch 布尔列。
func setKbSourceForceLatchTx(ctx context.Context, tx *gorm.DB, workspaceID, kbID, sourceID uuid.UUID, requestedForce bool, now time.Time) error {
	result := tx.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND kb_id = ? AND id = ?", workspaceID, kbID, sourceID).
		Updates(map[string]any{
			"sync_requested_force": requestedForce,
			"updated_at":           now,
		})
	if result.Error != nil {
		return translateDBError(result.Error, "更新源级 force latch 失败")
	}
	if result.RowsAffected != 1 {
		return domainerrors.ErrNotFound
	}
	return nil
}
