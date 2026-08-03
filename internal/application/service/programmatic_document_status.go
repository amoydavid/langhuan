package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ProgrammaticDocumentStatusReader 读取 Document 与 Job 的安全事实，按
// ResourceAccess 把绑定集合下推为 404 边界。
type ProgrammaticDocumentStatusReader interface {
	GetDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) (*dto.Document, error)
	GetJob(ctx context.Context, access value.ResourceAccess, jobID uuid.UUID) (*dto.Job, error)
}

// ProgrammaticDocumentStatusService 组合 Document/Job 查询并完成 lineage 校验，
// 供 MCP document_status 工具使用，避免 adapter 编排业务判断。
type ProgrammaticDocumentStatusService struct {
	documents ProgrammaticDocumentStatusReader
}

// NewProgrammaticDocumentStatusService 构造程序化文档状态服务。
func NewProgrammaticDocumentStatusService(documents ProgrammaticDocumentStatusReader) *ProgrammaticDocumentStatusService {
	return &ProgrammaticDocumentStatusService{documents: documents}
}

// ProgrammaticDocumentStatusInput 是程序化状态查询输入。
type ProgrammaticDocumentStatusInput struct {
	Access     value.ResourceAccess
	DocumentID uuid.UUID
	JobID      uuid.UUID // 可选
}

// ProgrammaticDocumentStatusResult 是程序化状态查询的安全输出。
type ProgrammaticDocumentStatusResult struct {
	Document *dto.Document `json:"document,omitempty"`
	Job      *dto.Job      `json:"job,omitempty"`
}

// Get 返回安全 Document（及可选 Job）。越界和 lineage 不匹配统一 not_found。
func (s *ProgrammaticDocumentStatusService) Get(ctx context.Context, input ProgrammaticDocumentStatusInput) (ProgrammaticDocumentStatusResult, error) {
	if input.Access.WorkspaceID == uuid.Nil || input.DocumentID == uuid.Nil {
		return ProgrammaticDocumentStatusResult{}, fmt.Errorf("%w: workspace_id/document_id 不能为空", domainerrors.ErrValidation)
	}
	doc, err := s.documents.GetDocument(ctx, input.Access, input.DocumentID)
	if err != nil {
		return ProgrammaticDocumentStatusResult{}, err
	}
	result := ProgrammaticDocumentStatusResult{Document: doc}
	if input.JobID != uuid.Nil {
		job, err := s.documents.GetJob(ctx, input.Access, input.JobID)
		if err != nil {
			return ProgrammaticDocumentStatusResult{}, err
		}
		// Job 必须属于同一 Workspace 和同一 Document lineage。
		if job.DocumentID != input.DocumentID {
			return ProgrammaticDocumentStatusResult{}, domainerrors.ErrNotFound
		}
		if !input.Access.Unrestricted && !input.Access.AllowsKnowledgeBase(job.KnowledgeBaseID) {
			return ProgrammaticDocumentStatusResult{}, domainerrors.ErrNotFound
		}
		result.Job = job
	}
	return result, nil
}
