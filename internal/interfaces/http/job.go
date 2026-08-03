package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

type JobQueryService interface {
	Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Job, error)
}

type jobHandler struct {
	service JobQueryService
}

// get returns a job. The workspace id comes from AuthContext (resolved by
// RequireWorkspace from the slug); cross-tenant and out-of-binding access is
// mapped to 404 by the service.
func (h jobHandler) get(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	job, err := h.service.Get(c.Request.Context(), authCtx.ResourceAccess(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, job)
}
