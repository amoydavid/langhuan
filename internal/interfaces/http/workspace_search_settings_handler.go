package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// WorkspaceSearchSettingsHTTPService 是 Workspace 查询策略的 handler 合同。
type WorkspaceSearchSettingsHTTPService interface {
	Get(context.Context, uuid.UUID) (*dto.WorkspaceSearchSettings, error)
	Update(context.Context, uuid.UUID, value.WorkspaceRole, service.UpdateWorkspaceSearchSettingsInput) (*dto.WorkspaceSearchSettings, error)
}

type workspaceSearchSettingsHandler struct {
	service WorkspaceSearchSettingsHTTPService
}

type workspaceSearchSettingsRequest struct {
	Rerank *workspaceSearchSettingsRerankRequest `json:"rerank"`
}

type workspaceSearchSettingsRerankRequest struct {
	Enabled       bool                    `json:"enabled"`
	ModelID       uuid.UUID               `json:"model_id"`
	CandidateTopK int                     `json:"candidate_top_k"`
	FailureMode   value.RerankFailureMode `json:"failure_mode"`
}

func (h workspaceSearchSettingsHandler) get(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	result, err := h.service.Get(c.Request.Context(), authCtx.WorkspaceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func (h workspaceSearchSettingsHandler) update(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	var request workspaceSearchSettingsRequest
	if err := decodeStrictJSON(c, &request); err != nil || request.Rerank == nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "检索策略参数无效")
		return
	}
	input := service.UpdateWorkspaceSearchSettingsInput{
		RerankEnabled: request.Rerank.Enabled,
		ModelID:       request.Rerank.ModelID,
		CandidateTopK: request.Rerank.CandidateTopK,
		FailureMode:   request.Rerank.FailureMode,
		ActorID:       authCtx.UserID,
	}
	result, err := h.service.Update(c.Request.Context(), authCtx.WorkspaceID, authCtx.Role, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}
