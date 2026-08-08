package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SourceCleanupDBStore 实现 appservice.SourceCleanupStore。
// GetSourceCleanupJob/Mark* 在 Workspace 作用域事务内执行；ListPendingSourceCleanupJobs
// 读取当前 DB 可见（受 RLS / 已设置 workspace context 限制）的 pending source_cleanup Job。
type SourceCleanupDBStore struct {
	db *gorm.DB
}

// NewSourceCleanupStore 构造一个 SourceCleanupDBStore。
func NewSourceCleanupStore(database *gorm.DB) *SourceCleanupDBStore {
	return &SourceCleanupDBStore{db: database}
}

// GetSourceCleanupJob 读取 cleanup Job（workspace 作用域）并从 payload 解析对象 key 列表。
func (s *SourceCleanupDBStore) GetSourceCleanupJob(
	ctx context.Context, workspaceID, jobID uuid.UUID,
) (*model.Job, []appservice.CleanupObject, error) {
	var row JobRow
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		if err := tx.
			Where("workspace_id = ? AND id = ? AND type = ?", workspaceID, jobID, model.SourceCleanupJobType).
			First(&row).Error; err != nil {
			return translateDBError(err, "读取 source_cleanup Job 失败")
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	job := jobV2FromRow(&row)
	objects := cleanupObjectsFromPayload(row.Payload)
	return job, objects, nil
}

// MarkSourceCleanupJobSucceeded 标记 cleanup Job 成功（completed，与 DocumentTask 约定一致）。
func (s *SourceCleanupDBStore) MarkSourceCleanupJobSucceeded(
	ctx context.Context, workspaceID, jobID uuid.UUID,
) error {
	return s.updateJob(ctx, workspaceID, jobID, map[string]any{
		"status": string(value.JobStatusCompleted), "error_class": "", "error_message": "",
		"updated_at": time.Now().UTC(),
	}, "标记 source_cleanup 成功失败")
}

// MarkSourceCleanupJobFailed 标记 cleanup Job 失败（保留可重试语义）。
func (s *SourceCleanupDBStore) MarkSourceCleanupJobFailed(
	ctx context.Context, workspaceID, jobID uuid.UUID, message string,
) error {
	return s.updateJob(ctx, workspaceID, jobID, map[string]any{
		"status": string(value.JobStatusFailed), "error_message": message,
		"updated_at": time.Now().UTC(),
	}, "标记 source_cleanup 失败失败")
}

// ListPendingSourceCleanupJobs 列出当前 DB 可见且 status=pending 的 source_cleanup Job。
// scheduler 启动/周期 Tick 时调用；无 pending 时返回空切片（nil 错误）。
func (s *SourceCleanupDBStore) ListPendingSourceCleanupJobs(ctx context.Context) ([]appservice.DueCleanupJob, error) {
	var rows []JobRow
	if err := s.db.WithContext(ctx).
		Select("workspace_id, knowledge_base_id, id").
		Where("type = ? AND status = ?", model.SourceCleanupJobType, string(value.JobStatusPending)).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, translateDBError(err, "列出 pending source_cleanup Job 失败")
	}
	result := make([]appservice.DueCleanupJob, 0, len(rows))
	for _, row := range rows {
		result = append(result, appservice.DueCleanupJob{
			WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID, JobID: row.ID,
		})
	}
	return result, nil
}

// updateJob 在 Workspace 作用域事务内更新 cleanup Job 终态。
// 仅当 RowsAffected==1 视为成功，否则返回 ErrNotFound（job 不存在或已不在该 workspace）。
func (s *SourceCleanupDBStore) updateJob(
	ctx context.Context, workspaceID, jobID uuid.UUID,
	updates map[string]any, description string,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		result := tx.Model(&JobRow{}).
			Where("workspace_id = ? AND id = ? AND type = ?", workspaceID, jobID, model.SourceCleanupJobType).
			Updates(updates)
		if result.Error != nil {
			return translateDBError(result.Error, description)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: source_cleanup Job 不存在: %s", domainerrors.ErrNotFound, jobID)
		}
		return nil
	})
}

// cleanupObjectsFromPayload 从 Job payload 的 "objects" 字段解析对象 key 列表。
// payload 由 cleanupObjectsToPayload 写入，结构为 [{"key": "...", "store": "..."}]。
// 容错处理：单条缺失/类型错误时跳过该条，不使整个清理任务失败。
func cleanupObjectsFromPayload(payload map[string]any) []appservice.CleanupObject {
	if payload == nil {
		return nil
	}
	rawList, ok := payload["objects"].([]any)
	if !ok {
		return nil
	}
	objects := make([]appservice.CleanupObject, 0, len(rawList))
	for _, raw := range rawList {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := entry["key"].(string)
		store, _ := entry["store"].(string)
		if key == "" {
			continue
		}
		objects = append(objects, appservice.CleanupObject{Key: key, Store: store})
	}
	return objects
}
