package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
)

// WorkspaceService is the handler-side workspace service interface. It includes
// GetBySlug so the SAME dependency can be passed to both the workspace handlers
// and the RequireWorkspace middleware (which needs a WorkspaceResolver). The
// concrete *service.WorkspaceService satisfies this interface.
type WorkspaceService interface {
	Create(ctx context.Context, input service.CreateWorkspaceInput) (*dto.Workspace, error)
	CreateForPlatformAdmin(ctx context.Context, input service.CreateWorkspaceInput, creatorUserID uuid.UUID, creatorIsPlatformAdmin bool) (*dto.Workspace, error)
	Get(ctx context.Context, id uuid.UUID) (*dto.Workspace, error)
	GetBySlug(ctx context.Context, slug string) (*dto.Workspace, error)
}

type createWorkspaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type workspaceHandler struct {
	service WorkspaceService
}

// create (platform admin) creates a workspace and the creator's owner membership
// in the same transaction. The actor context comes from AuthContext.
func (h workspaceHandler) create(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	var req createWorkspaceRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	if strings.TrimSpace(req.Slug) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "slug 不能为空")
		return
	}

	ws, err := h.service.CreateForPlatformAdmin(c.Request.Context(), service.CreateWorkspaceInput{
		Name: req.Name,
		Slug: req.Slug,
	}, authCtx.UserID, authCtx.IsPlatformAdmin)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, ws)
}

// get returns the workspace resolved by RequireWorkspace (id comes from AuthContext).
func (h workspaceHandler) get(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	ws, err := h.service.Get(c.Request.Context(), authCtx.WorkspaceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, ws)
}
