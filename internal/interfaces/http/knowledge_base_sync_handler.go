package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSyncService 是手动触发来源同步的 HTTP 层契约。
// 生产装配用一个薄适配器包裹 *service.SourceSyncService.EnqueueSync 并把 *model.Job 转为 *dto.Job。
type KnowledgeBaseSyncService interface {
	// EnqueueSync 为指定知识库创建并入队一个 source_sync 任务，返回创建的 Job（不含正文/凭证）。
	EnqueueSync(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) (*dto.Job, error)
}

// knowledgeBaseSyncHandler 处理手动触发来源同步的 API。
type knowledgeBaseSyncHandler struct {
	service KnowledgeBaseSyncService
}

// sync 手动触发一个知识库的全量来源同步。
//
// POST /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/sync
//
// 鉴权：要求 workspace admin/owner（在路由层用 RequireWorkspaceRole(RoleAdmin) 强制）。
// 无请求体；返回 202 + {"job_id": ...}。
func (h knowledgeBaseSyncHandler) sync(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if !authCtx.Role.AtLeast(value.RoleAdmin) {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	job, err := h.service.EnqueueSync(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, gin.H{"job_id": job.ID})
}
