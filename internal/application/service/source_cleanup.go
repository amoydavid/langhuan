package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/storage"
)

// SourceCleanupObjectBatchSize 是单个 source_cleanup Job payload 携带的对象 key 上限。
// 超过该上限时按稳定 key 顺序拆批，每批一个 Job，避免单个清理任务过大或队列消息膨胀。
const SourceCleanupObjectBatchSize = 100

// DueCleanupJob 是 scheduler 扫描到的待恢复派发的 source_cleanup Job 的最小 lineage。
// 由 SourceCleanupStore.ListPendingSourceCleanupJobs 返回。
type DueCleanupJob struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	JobID           uuid.UUID
}

// SourceCleanupStore 提供来源对象清理所需的持久化端口（DB 实现）。
//
//   - GetSourceCleanupJob 读取 cleanup Job 及其 payload 中的对象 key 列表；
//   - MarkSourceCleanupJobSucceeded/Failed 推进 Job 终态；
//   - ListPendingSourceCleanupJobs 扫描 pending 的 cleanup Job 供 scheduler 恢复派发。
type SourceCleanupStore interface {
	// GetSourceCleanupJob 读取 cleanup Job（workspace 作用域）并从其 payload 解析对象 key 列表。
	GetSourceCleanupJob(ctx context.Context, workspaceID, jobID uuid.UUID) (*model.Job, []CleanupObject, error)
	// MarkSourceCleanupJobSucceeded 标记 cleanup Job 成功（completed）。
	MarkSourceCleanupJobSucceeded(ctx context.Context, workspaceID, jobID uuid.UUID) error
	// MarkSourceCleanupJobFailed 标记 cleanup Job 失败（保留可重试语义）。
	MarkSourceCleanupJobFailed(ctx context.Context, workspaceID, jobID uuid.UUID, message string) error
	// ListPendingSourceCleanupJobs 列出当前 Workspace 可见且 status=pending 的 source_cleanup Job。
	ListPendingSourceCleanupJobs(ctx context.Context) ([]DueCleanupJob, error)
}

// SourceCleanupServiceDeps 注入来源对象清理的全部依赖。
type SourceCleanupServiceDeps struct {
	// Store 读取 cleanup Job + object keys，推进 Job 状态。
	Store SourceCleanupStore
	// RawStore 删除 raw/parser 类对象（两者均落同一对象存储 bucket）。
	RawStore storage.RawDocumentStore
	// AssetStore 删除 asset 类对象。
	AssetStore storage.AssetStore
	// Logger 输出清理日志（不含正文/凭证）。
	Logger Logger
}

// SourceCleanupService 执行单个 source_cleanup Job：删除每个外部对象（幂等），推进 Job 状态。
type SourceCleanupService struct {
	store      SourceCleanupStore
	rawStore   storage.RawDocumentStore
	assetStore storage.AssetStore
	logger     Logger
}

// NewSourceCleanupService 构造一个 SourceCleanupService。
func NewSourceCleanupService(deps SourceCleanupServiceDeps) *SourceCleanupService {
	logger := deps.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	return &SourceCleanupService{
		store:      deps.Store,
		rawStore:   deps.RawStore,
		assetStore: deps.AssetStore,
		logger:     logger,
	}
}

// Run 执行单个 cleanup Job（spec 9.2）：加载 Job + object keys → 逐个幂等删除 → 推进 Job 终态。
//
// 幂等性：对象已不存在（storage.ErrObjectNotFound）视为成功，允许重复执行或 scheduler 重派。
// 任一非 not-found 错误都会把 Job 标记为 failed，交由 asynq/worker 重试；全部成功则标记 succeeded。
func (s *SourceCleanupService) Run(ctx context.Context, workspaceID, kbID, jobID uuid.UUID) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || jobID == uuid.Nil {
		return fmt.Errorf("%w: SourceCleanup Run lineage 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil {
		return fmt.Errorf("%w: SourceCleanup Store 未配置", domainerrors.ErrValidation)
	}

	job, objects, err := s.store.GetSourceCleanupJob(ctx, workspaceID, jobID)
	if err != nil {
		return fmt.Errorf("读取 source_cleanup Job 失败: %w", err)
	}
	if job == nil {
		return fmt.Errorf("%w: source_cleanup Job 不存在: %s", domainerrors.ErrNotFound, jobID)
	}

	// 已终结的 Job 直接幂等返回（重复派发/重试到达已完成任务）。
	switch job.Status {
	case value.JobStatusSucceeded, value.JobStatusCompleted:
		s.logger.Info("source_cleanup Job 已完成，跳过",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"job_id", jobID.String(),
		)
		return nil
	}

	var failed []string
	for _, obj := range objects {
		if err := s.deleteObject(ctx, obj); err != nil {
			failed = append(failed, fmt.Sprintf("%s[%s]: %s", obj.Store, obj.Key, err.Error()))
		}
	}

	if len(failed) > 0 {
		message := fmt.Sprintf("清理对象部分失败: %s", strings.Join(failed, "; "))
		if markErr := s.store.MarkSourceCleanupJobFailed(ctx, workspaceID, jobID, message); markErr != nil {
			s.logger.Error("标记 source_cleanup 失败状态失败",
				"workspace_id", workspaceID.String(),
				"knowledge_base_id", kbID.String(),
				"job_id", jobID.String(),
				"error", markErr.Error(),
			)
		}
		// 返回原始错误（非永久性），让 asynq 按重试策略重试。
		return errors.New(message)
	}

	if err := s.store.MarkSourceCleanupJobSucceeded(ctx, workspaceID, jobID); err != nil {
		s.logger.Error("标记 source_cleanup 成功状态失败",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"job_id", jobID.String(),
			"error", err.Error(),
		)
		return fmt.Errorf("标记 source_cleanup 成功状态失败: %w", err)
	}
	s.logger.Info("source_cleanup 清理完成",
		"workspace_id", workspaceID.String(),
		"knowledge_base_id", kbID.String(),
		"job_id", jobID.String(),
		"object_count", len(objects),
	)
	return nil
}

// deleteObject 按 Store 字段路由到对应 adapter 删除对象。
// raw/parser 路由到 RawStore（两者落同一 bucket）；asset 路由到 AssetStore。
// storage.ErrObjectNotFound 视为幂等成功（对象已被前次清理删除）。
func (s *SourceCleanupService) deleteObject(ctx context.Context, obj CleanupObject) error {
	switch obj.Store {
	case "raw", "parser":
		if s.rawStore == nil {
			return fmt.Errorf("raw store 未配置，无法删除 %s", obj.Key)
		}
		err := s.rawStore.Delete(ctx, obj.Key)
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil
		}
		return err
	case "asset":
		if s.assetStore == nil {
			return fmt.Errorf("asset store 未配置，无法删除 %s", obj.Key)
		}
		err := s.assetStore.Delete(ctx, obj.Key)
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil
		}
		return err
	default:
		return fmt.Errorf("未知的对象 store 类型 %q（key=%s）", obj.Store, obj.Key)
	}
}
