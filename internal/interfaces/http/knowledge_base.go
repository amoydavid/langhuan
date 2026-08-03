package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

type KnowledgeBaseService interface {
	Create(ctx context.Context, input service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error)
	Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*dto.KnowledgeBase, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]*dto.KnowledgeBase, error)
	UpdateBasics(ctx context.Context, input service.UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error)
}

type createKnowledgeBaseRequest struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	EmbeddingModelID uuid.UUID              `json:"embedding_model_id"`
	ChunkingConfig   *chunkingConfigRequest `json:"chunking_config"`
}

type chunkingConfigRequest struct {
	ChunkSize    int `json:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap"`
}

type updateKnowledgeBaseBasicsRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type knowledgeBaseHandler struct {
	service KnowledgeBaseService
}

// create (workspace member+) creates a knowledge base. The workspace id comes from
// AuthContext (resolved by RequireWorkspace from the slug).
func (h knowledgeBaseHandler) create(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	var req createKnowledgeBaseRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	if req.EmbeddingModelID == uuid.Nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "embedding_model_id 必须是有效 UUID")
		return
	}

	input := service.CreateKnowledgeBaseInput{
		WorkspaceID: authCtx.WorkspaceID, Name: req.Name, Description: req.Description,
		EmbeddingModelID: req.EmbeddingModelID,
	}
	if authCtx.IsAPIKey() {
		apiKeyID := authCtx.PrincipalID
		input.CallerAPIKeyID = &apiKeyID
	}
	if req.ChunkingConfig != nil {
		chunking := value.ChunkingConfig{
			ChunkSize:    req.ChunkingConfig.ChunkSize,
			ChunkOverlap: req.ChunkingConfig.ChunkOverlap,
		}
		input.ChunkingConfig = &chunking
	}
	kb, err := h.service.Create(c.Request.Context(), input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, kb)
}

func (h knowledgeBaseHandler) list(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	items, err := h.service.List(c.Request.Context(), authCtx.WorkspaceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if items == nil {
		items = make([]*dto.KnowledgeBase, 0)
	}
	c.JSON(stdhttp.StatusOK, items)
}

// get returns a knowledge base. The workspace id comes from AuthContext; cross-tenant
// access is mapped to 404 by the service (workspace id mismatch).
func (h knowledgeBaseHandler) get(c *gin.Context) {
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
	kb, err := h.service.Get(c.Request.Context(), authCtx.WorkspaceID, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, kb)
}

func (h knowledgeBaseHandler) patch(c *gin.Context) {
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
	var req updateKnowledgeBaseBasicsRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if req.Name == nil && req.Description == nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "至少提供 name 或 description")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	result, err := h.service.UpdateBasics(c.Request.Context(), service.UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: id,
		Name: req.Name, Description: req.Description, ActorRole: authCtx.Role,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}
