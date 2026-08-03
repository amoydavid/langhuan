package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
)

// WorkspaceReadinessHTTPService is the Workspace setup/readiness query contract.
type WorkspaceReadinessHTTPService interface {
	Get(context.Context, uuid.UUID) (*dto.WorkspaceReadiness, error)
}

type workspaceReadinessHandler struct{ service WorkspaceReadinessHTTPService }

func (h workspaceReadinessHandler) get(c *gin.Context) {
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
