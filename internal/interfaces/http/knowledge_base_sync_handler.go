package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSyncService 是手动触发来源同步的 HTTP 层契约。
// 生产装配用一个薄适配器包裹 *service.SourceSyncService.EnqueueSync 并把 *model.Job 转为 *dto.Job。
type KnowledgeBaseSyncService interface {
	// EnqueueSync 为指定知识库创建并入队一个 source_sync 任务，返回创建的 Job（不含正文/凭证）。
	// options.Force=true 表示强制全量同步（忽略内容 hash 去重）。
	EnqueueSync(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID, options service.SyncOptions) (*dto.Job, error)
}

// knowledgeBaseSyncHandler 处理手动触发来源同步的 API。
type knowledgeBaseSyncHandler struct {
	service KnowledgeBaseSyncService
}

// syncRequest 是手动同步的可选请求体；空 body 视为 Force=false（spec 8.1）。
// 未知字段由 decodeStrictJSON 拒绝（400）。
type syncRequest struct {
	Force bool `json:"force"`
}

// sync 手动触发一个知识库的全量来源同步。
//
// POST /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/sync
//
// 鉴权：要求 workspace admin/owner（在路由层用 RequireWorkspaceRole(RoleAdmin) 强制）。
// 请求体可选：{"force":true} 表示强制同步；空 body 为普通同步。返回 202 + {"job_id": ...}。
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

	options := service.SyncOptions{}
	// 请求体可选：空 body 跳过解析；非空 body 用 strict JSON 解析（拒绝未知字段）。
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "invalid_body", "读取请求体失败")
		return
	}
	if len(bytes.TrimSpace(bodyBytes)) > 0 {
		var req syncRequest
		decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeError(c, stdhttp.StatusBadRequest, "invalid_body", "请求体格式错误")
			return
		}
		// 拒绝多个 JSON 对象。
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeError(c, stdhttp.StatusBadRequest, "invalid_body", "请求只能包含一个 JSON 对象")
			return
		}
		options.Force = req.Force
	}

	job, err := h.service.EnqueueSync(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID, options)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, gin.H{"job_id": job.ID})
}
