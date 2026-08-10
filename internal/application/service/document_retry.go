package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

// JobRevision 携带 job 及其关联 revision 的定位信息，供按 job 重试时使用。
type JobRevision struct {
	JobID           uuid.UUID
	KnowledgeBaseID uuid.UUID
	DocumentID      uuid.UUID
	RevisionID      uuid.UUID
}

// ResetFailedRevisionRequest 复位一个 failed revision 以重跑 parse。
type ResetFailedRevisionRequest struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	DocumentID      uuid.UUID
	RevisionID      uuid.UUID
	// GenerationID 是复位时当前 active generation，写入 parse job payload，
	// 保证 worker 的 generationIDFromJobPayload 校验通过（reindex 后重试场景）。
	GenerationID uuid.UUID
}

// DocumentRetryTx 定位失败 revision 并在事务内原子复位。
type DocumentRetryTx interface {
	GetKnowledgeBase(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
	// GetLatestRevision 返回 document 的最新 revision（按 revision_no DESC）。
	GetLatestRevision(context.Context, uuid.UUID) (*model.DocumentRevision, error)
	// GetJobRevision 按 job_id 定位其关联 revision 与 KB lineage。
	GetJobRevision(context.Context, uuid.UUID) (*JobRevision, error)
	// ResetFailedRevision 复位 failed revision 到 pending 并复位/新建 parse Job。
	// revision 非 failed 时返回 ErrNotRetryable；返回的 JobID 供调用方入队。
	ResetFailedRevision(context.Context, ResetFailedRevisionRequest) (uuid.UUID, error)
	// FailReset 在复位后入队失败时，把 revision 与 job 标回 failed（error_class=enqueue_error），
	// 保证用户可以再次重试，避免"revision 已 pending 但任务未入队"的永久卡死。
	FailReset(context.Context, ResetFailedRevisionRequest, uuid.UUID, string) error
}

// DocumentRetryStore 进入 Workspace 级别的失败重试事务。
type DocumentRetryStore interface {
	WithinWorkspace(ctx context.Context, workspaceID uuid.UUID, fn func(context.Context, DocumentRetryTx) error) error
}

// RetryResult 是一次失败重试返回给接口层的结果。
type RetryResult struct {
	JobID      uuid.UUID `json:"job_id"`
	RevisionID uuid.UUID `json:"revision_id"`
	DocumentID uuid.UUID `json:"document_id"`
}

// DocumentRetryService 编排失败 revision 的重试：定位 → 复位 → 入队。
// 角色校验由 HTTP/MCP 中间件保证：REST retry 路由挂 RequireAdminForSession（Session
// 要求 admin/owner，API Key 放行由 scope 控制）；MCP document_retry 工具由
// ScopeDocumentsWrite + KB 绑定集合控制。service 层只做 ResourceAccess 的 KB 绑定边界校验。
type DocumentRetryService struct {
	store  DocumentRetryStore
	queue  queue.JobQueue
	logger *slog.Logger
}

// DocumentRetryServiceDeps 装配失败重试服务。
type DocumentRetryServiceDeps struct {
	Store  DocumentRetryStore
	Queue  queue.JobQueue
	Logger *slog.Logger
}

func NewDocumentRetryService(deps DocumentRetryServiceDeps) *DocumentRetryService {
	return &DocumentRetryService{store: deps.Store, queue: deps.Queue, logger: deps.Logger}
}

// RetryDocument 重试 document 最新 revision。
// access 用于 API Key 主体把绑定集合下推为 404 边界。
func (s *DocumentRetryService) RetryDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) (*RetryResult, error) {
	if access.WorkspaceID == uuid.Nil || documentID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/document_id 不能为空", domainerrors.ErrValidation)
	}
	out, err := s.retryInWorkspace(ctx, access, func(tx DocumentRetryTx) (*retryPlan, error) {
		revision, err := tx.GetLatestRevision(ctx, documentID)
		if err != nil {
			return nil, err
		}
		if !access.Unrestricted && !access.AllowsKnowledgeBase(revision.KnowledgeBaseID) {
			return nil, domainerrors.ErrNotFound
		}
		return s.planReset(ctx, tx, revision.KnowledgeBaseID, documentID, revision.ID)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RetryJob 重试 job 关联的 revision。
func (s *DocumentRetryService) RetryJob(ctx context.Context, access value.ResourceAccess, jobID uuid.UUID) (*RetryResult, error) {
	if access.WorkspaceID == uuid.Nil || jobID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/job_id 不能为空", domainerrors.ErrValidation)
	}
	out, err := s.retryInWorkspace(ctx, access, func(tx DocumentRetryTx) (*retryPlan, error) {
		jr, err := tx.GetJobRevision(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if !access.Unrestricted && !access.AllowsKnowledgeBase(jr.KnowledgeBaseID) {
			return nil, domainerrors.ErrNotFound
		}
		return s.planReset(ctx, tx, jr.KnowledgeBaseID, jr.DocumentID, jr.RevisionID)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// retryPlan 携带一次复位所需的事务内结果。
type retryPlan struct {
	kbID, genID, revisionID, documentID, jobID uuid.UUID
}

// retryInWorkspace 统一执行"事务内定位+复位 → 事务外入队"。
func (s *DocumentRetryService) retryInWorkspace(
	ctx context.Context, access value.ResourceAccess,
	locate func(DocumentRetryTx) (*retryPlan, error),
) (*RetryResult, error) {
	var plan *retryPlan
	err := s.store.WithinWorkspace(ctx, access.WorkspaceID, func(txCtx context.Context, tx DocumentRetryTx) error {
		p, err := locate(tx)
		if err != nil {
			return err
		}
		plan = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.enqueueParseStart(ctx, access.WorkspaceID, plan.kbID, plan.documentID, plan.revisionID, plan.genID, plan.jobID); err != nil {
		// 补偿：复位已提交（revision/job=pending）但任务未入队。把 revision/job 标回 failed，
		// 允许再次重试；否则 revision 已 pending，下次 retry 会拿 ErrNotRetryable 永久卡死。
		compErr := s.store.WithinWorkspace(ctx, access.WorkspaceID, func(txCtx context.Context, tx DocumentRetryTx) error {
			return tx.FailReset(txCtx, ResetFailedRevisionRequest{
				WorkspaceID: access.WorkspaceID, KnowledgeBaseID: plan.kbID,
				DocumentID: plan.documentID, RevisionID: plan.revisionID, GenerationID: plan.genID,
			}, plan.jobID, err.Error())
		})
		if compErr != nil {
			return nil, errors.Join(err, fmt.Errorf("重试入队失败补偿落库失败: %w", compErr))
		}
		return nil, err
	}
	return &RetryResult{JobID: plan.jobID, RevisionID: plan.revisionID, DocumentID: plan.documentID}, nil
}

// planReset 在事务内校验 active generation 并复位 failed revision。
func (s *DocumentRetryService) planReset(ctx context.Context, tx DocumentRetryTx, kbID, documentID, revisionID uuid.UUID) (*retryPlan, error) {
	kb, err := tx.GetKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb.ActiveIndexGenerationID == nil || *kb.ActiveIndexGenerationID == uuid.Nil {
		return nil, fmt.Errorf("%w: 知识库缺少 active IndexGeneration", domainerrors.ErrValidation)
	}
	jobID, err := tx.ResetFailedRevision(ctx, ResetFailedRevisionRequest{
		WorkspaceID:     kb.WorkspaceID,
		KnowledgeBaseID: kb.ID,
		DocumentID:      documentID,
		RevisionID:      revisionID,
		GenerationID:    *kb.ActiveIndexGenerationID,
	})
	if err != nil {
		return nil, err
	}
	return &retryPlan{
		kbID: kb.ID, genID: *kb.ActiveIndexGenerationID,
		revisionID: revisionID, documentID: documentID, jobID: jobID,
	}, nil
}

// enqueueParseStart 构造稳定 TaskID 入队 document_parse_start，保证幂等去重。
func (s *DocumentRetryService) enqueueParseStart(ctx context.Context, workspaceID, knowledgeBaseID, documentID, revisionID, generationID, jobID uuid.UUID) error {
	payload, err := json.Marshal(map[string]string{
		"workspace_id":         workspaceID.String(),
		"knowledge_base_id":    knowledgeBaseID.String(),
		"document_id":          documentID.String(),
		"document_revision_id": revisionID.String(),
		"generation_id":        generationID.String(),
		"job_id":               jobID.String(),
	})
	if err != nil {
		return err
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type:    documentParseStartJobType,
		Payload: payload,
		TaskID:  queue.DocumentTaskID(documentParseStartJobType, workspaceID, revisionID, generationID),
	}); err != nil {
		return fmt.Errorf("入队文档重试任务失败: %w", err)
	}
	if s.logger != nil {
		s.logger.InfoContext(ctx, "document.retry.enqueued",
			"workspace_id", workspaceID,
			"document_id", documentID,
			"revision_id", revisionID,
			"job_id", jobID,
		)
	}
	return nil
}
