package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件实现 Meta Scheduler 的 web 内容源调度（v1.3.0 spec §9.1）store 层读写：
// 到期扫描、限流计数（源级/connection 级/全局在飞）与源级 source_sync Job 的
// 幂等创建/入队失败标记/next_sync_at 推进。全部方法挂在 SourceSyncDBStore 上，
// 与执行侧运行态读写（kb_source_sync_store.go）分开；事务边界沿用
// WorkspaceTxRunner（kb_sources 复合外键携带 workspace lineage）。

// ListDueWebSources 列出到期 web 源（kind=web、status=active、未软删、
// next_sync_at 非空且 <= now）。next_sync_at 为 NULL 的源不视为到期（无 cron 仅手动触发）。
// 全部为普通列比较，无 JSON 函数，PG/SQLite 方言共用；结果按 (workspace, kb, id)
// 排序保证 Meta Scheduler 分组稳定。
func (s *SourceSyncDBStore) ListDueWebSources(ctx context.Context, now time.Time) ([]service.DueWebSource, error) {
	type dueRow struct {
		WorkspaceID     uuid.UUID `gorm:"column:workspace_id"`
		KnowledgeBaseID uuid.UUID `gorm:"column:kb_id"`
		ID              uuid.UUID `gorm:"column:id"`
		Config          JSONMap   `gorm:"column:config"`
	}
	var rows []dueRow
	err := s.db.WithContext(ctx).Table("kb_sources").
		Select("workspace_id, kb_id, id, config").
		Where("kind = ?", string(value.KbSourceKindWeb)).
		Where("status = ?", string(value.KbSourceStatusActive)).
		Where("deleted_at IS NULL").
		Where("next_sync_at IS NOT NULL AND next_sync_at <= ?", now.UTC()).
		Order("workspace_id, kb_id, id").
		Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "列出到期 web 内容源失败")
	}
	result := make([]service.DueWebSource, 0, len(rows))
	for _, row := range rows {
		config := normalizedDomainMap(row.Config)
		// 非法 connection_id 按"无连接"归组（http 引擎同形）；配置合法性由同步执行路径校验。
		connectionID, _ := service.WebFetchConnectionID(config)
		result = append(result, service.DueWebSource{
			WorkspaceID:     row.WorkspaceID,
			KnowledgeBaseID: row.KnowledgeBaseID,
			ID:              row.ID,
			ConnectionID:    connectionID,
			Config:          config,
		})
	}
	return result, nil
}

// CountActiveBySource 统计某源进行中的源级 source_sync 任务数（pending/running），
// 走 000025 的 idx_jobs_source_active 部分索引。
func (s *SourceSyncDBStore) CountActiveBySource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (int, error) {
	return countActiveSourceSyncJobs(ctx, s.db, workspaceID, kbID, sourceID)
}

// countActiveSourceSyncJobs 是源级 active source_sync 任务计数的共享实现
// （SourceSyncDBStore 调度侧与 KbSourceDBRepository 管理面删除守卫共用，单一 SQL）。
func countActiveSourceSyncJobs(ctx context.Context, db *gorm.DB, workspaceID, kbID, sourceID uuid.UUID) (int, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return 0, fmt.Errorf("%w: CountActiveBySource lineage 不能为空", domainerrors.ErrValidation)
	}
	var count int64
	err := db.WithContext(ctx).Model(&JobRow{}).
		Where("workspace_id = ? AND knowledge_base_id = ? AND kb_source_id = ? AND type = ? AND status IN ?",
			workspaceID, kbID, sourceID, model.SourceSyncJobType,
			[]string{string(value.JobStatusPending), string(value.JobStatusRunning)},
		).Count(&count).Error
	if err != nil {
		return 0, translateDBError(err, "统计源进行中任务失败")
	}
	return int(count), nil
}

// CountActiveWebSourceJobs 全局统计目标为 web 源（kb_source_id 非空）的 active
// source_sync 任务数（pending/running，跨 workspace）——web 源的全局在飞上限
// 只封顶 web 源任务对 worker 槽的占用；飞书任务由 connection 限额管辖，不占此配额。
func (s *SourceSyncDBStore) CountActiveWebSourceJobs(ctx context.Context) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&JobRow{}).
		Where("kb_source_id IS NOT NULL AND type = ? AND status IN ?",
			model.SourceSyncJobType,
			[]string{string(value.JobStatusPending), string(value.JobStatusRunning)},
		).Count(&count).Error
	if err != nil {
		return 0, translateDBError(err, "统计 web 源全局进行中任务失败")
	}
	return int(count), nil
}

// RequestSourceSyncForSource 在源行锁定事务内：源级 force latch = old OR requestedForce；
// 存在该源 pending/running 的源级 source_sync Job 则复用（created=false），否则新建
// （created=true，行上标注源 config 的 fetch.connection_id）。语义与 KB 级
// RequestSourceSync 同构：同源串行的原子保证（spec §9.1 同源单 active）。
func (s *SourceSyncDBStore) RequestSourceSyncForSource(
	ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, requestedForce bool,
) (*model.Job, bool, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, false, fmt.Errorf("%w: RequestSourceSyncForSource lineage 不能为空", domainerrors.ErrValidation)
	}
	var (
		job     *model.Job
		created bool
	)
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		row, err := lockKbSourceForUpdate(ctx, tx, workspaceID, kbID, sourceID)
		if err != nil {
			return err
		}
		// latch = old OR requestedForce（force 请求"粘住"直到 worker 消费）。
		if err := setKbSourceForceLatchTx(ctx, tx, workspaceID, kbID, sourceID,
			row.SyncRequestedForce || requestedForce, time.Now().UTC()); err != nil {
			return err
		}

		// 复用该源进行中的源级 source_sync Job（pending/running）。
		var existing JobRow
		findErr := tx.WithContext(ctx).
			Where("workspace_id = ? AND knowledge_base_id = ? AND kb_source_id = ? AND type = ? AND status IN ?",
				workspaceID, kbID, sourceID, model.SourceSyncJobType,
				[]string{string(value.JobStatusPending), string(value.JobStatusRunning)}).
			Order("created_at DESC, id DESC").
			First(&existing).Error
		if findErr == nil {
			job = jobV2FromRow(&existing)
			created = false
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return translateDBError(findErr, "查找源进行中 source_sync Job 失败")
		}

		// 新建源级 source_sync Job，标注源引用的连接（connection 组限流统计用）。
		newJob, err := model.NewJob(model.NewJobInput{
			WorkspaceID: workspaceID, KnowledgeBaseID: kbID, KbSourceID: sourceID,
			SourceConnectionID: sourceConnectionIDFromConfig(row.Config),
			Type:               model.SourceSyncJobType, Status: value.JobStatusPending,
		})
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(jobV2ToRow(newJob)).Error; err != nil {
			return translateDBError(err, "创建源级 source_sync Job 失败")
		}
		job = newJob
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return job, created, nil
}

// FailSourceSyncEnqueueForSource 标记源级 source_sync Job 首次入队失败（仅 pending
// 可转换，幂等），保留 force latch 与到期状态供 Meta Scheduler 重试派发。
func (s *SourceSyncDBStore) FailSourceSyncEnqueueForSource(
	ctx context.Context, workspaceID, kbID, sourceID, jobID uuid.UUID, message string,
) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil || jobID == uuid.Nil {
		return fmt.Errorf("%w: FailSourceSyncEnqueueForSource lineage 不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("%w: FailSourceSyncEnqueueForSource message 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND kb_source_id = ? AND id = ? AND type = ? AND status = ?",
				workspaceID, kbID, sourceID, jobID, model.SourceSyncJobType, string(value.JobStatusPending)).
			Updates(map[string]any{
				"status":        string(value.JobStatusFailed),
				"error_class":   "enqueue_failed",
				"error_message": message,
				"updated_at":    now,
			})
		if result.Error != nil {
			return translateDBError(result.Error, "标记源级 source_sync 入队失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		// 不动 latch：调度器重试派发时仍能感知未消费的 force 请求。
		return nil
	})
}

// UpdateWebSourceNextSyncAt 更新 kb_sources.next_sync_at 列；nextSyncAt 为零值时置 NULL
// （停止定时调度，仅手动触发）。
func (s *SourceSyncDBStore) UpdateWebSourceNextSyncAt(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, nextSyncAt time.Time) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return fmt.Errorf("%w: UpdateWebSourceNextSyncAt lineage 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var next *time.Time
		if !nextSyncAt.IsZero() {
			t := nextSyncAt.UTC()
			next = &t
		}
		result := tx.WithContext(ctx).Model(&KbSourceRow{}).
			Where("workspace_id = ? AND kb_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, kbID, sourceID).
			Updates(map[string]any{
				"next_sync_at": next,
				"updated_at":   time.Now().UTC(),
			})
		if result.Error != nil {
			return translateDBError(result.Error, "更新内容源 next_sync_at 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}
