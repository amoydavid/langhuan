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

// ChunkRevisionHTTPService is the Chunk query and edit use-case contract.
type ChunkRevisionHTTPService interface {
	Get(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*dto.Chunk, error)
	List(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]*dto.ChunkRevision, error)
	Create(context.Context, service.CreateChunkRevisionInput) (*dto.ChunkRevision, error)
}

type chunkRevisionHandler struct{ service ChunkRevisionHTTPService }

type createChunkRevisionRequest struct {
	BaseRevisionID uuid.UUID `json:"base_revision_id"`
	Content        *string   `json:"content"`
	ContextHeader  *string   `json:"context_header"`
	Enabled        *bool     `json:"enabled"`
}

func (h chunkRevisionHandler) get(c *gin.Context) {
	authCtx, knowledgeBaseID, chunkID, ok := chunkRouteContext(c)
	if !ok {
		return
	}
	result, err := h.service.Get(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID, chunkID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func (h chunkRevisionHandler) list(c *gin.Context) {
	authCtx, knowledgeBaseID, chunkID, ok := chunkRouteContext(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID, chunkID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func (h chunkRevisionHandler) create(c *gin.Context) {
	authCtx, knowledgeBaseID, chunkID, ok := chunkRouteContext(c)
	if !ok {
		return
	}
	if !authCtx.Role.AtLeast(value.RoleAdmin) {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	var request createChunkRevisionRequest
	if err := decodeStrictJSON(c, &request); err != nil || request.BaseRevisionID == uuid.Nil ||
		request.Content == nil || request.ContextHeader == nil || request.Enabled == nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "base_revision_id/content/context_header/enabled 无效")
		return
	}
	result, err := h.service.Create(c.Request.Context(), service.CreateChunkRevisionInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, ChunkID: chunkID,
		BaseRevisionID: request.BaseRevisionID, Content: *request.Content, ContextHeader: *request.ContextHeader,
		Enabled: *request.Enabled, EditorUserID: authCtx.UserID, ActorRole: authCtx.Role,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, result)
}

func chunkRouteContext(c *gin.Context) (value.AuthContext, uuid.UUID, uuid.UUID, bool) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return value.AuthContext{}, uuid.Nil, uuid.Nil, false
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return value.AuthContext{}, uuid.Nil, uuid.Nil, false
	}
	chunkID, ok := parseUUIDParam(c, "chunk_id")
	if !ok {
		return value.AuthContext{}, uuid.Nil, uuid.Nil, false
	}
	return authCtx, knowledgeBaseID, chunkID, true
}
