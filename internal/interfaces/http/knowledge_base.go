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
	Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.KnowledgeBase, error)
	List(ctx context.Context, access value.ResourceAccess) ([]*dto.KnowledgeBase, error)
	UpdateBasics(ctx context.Context, input service.UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error)
}

type createKnowledgeBaseRequest struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	EmbeddingModelID   uuid.UUID              `json:"embedding_model_id"`
	ChunkingConfig     *chunkingConfigRequest `json:"chunking_config"`
	SourceType         string                 `json:"source_type"`
	SourceConfig       map[string]any         `json:"source_config"`
	SourceConnectionID *uuid.UUID             `json:"source_connection_id"`
}

type chunkingConfigRequest struct {
	ChunkSize         int                    `json:"chunk_size"`
	ChunkOverlap      int                    `json:"chunk_overlap"`
	Strategy          value.ChunkingStrategy `json:"strategy"`
	EnableParentChild *bool                  `json:"enable_parent_child"`
	ParentChunkSize   int                    `json:"parent_chunk_size"`
	ChildChunkSize    int                    `json:"child_chunk_size"`
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
	// 来源字段透传：飞书来源需要 source_type/source_connection_id（及可选 source_config）。
	if strings.TrimSpace(req.SourceType) != "" {
		sourceType := value.KnowledgeBaseSourceType(strings.TrimSpace(req.SourceType))
		if !sourceType.IsValid() {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "source_type 必须是 upload/feishu_drive/feishu_wiki")
			return
		}
		if sourceType.IsFeishu() && (req.SourceConnectionID == nil || *req.SourceConnectionID == uuid.Nil) {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "飞书来源必须提供 source_connection_id")
			return
		}
		input.SourceType = sourceType
		input.SourceConfig = req.SourceConfig
		input.SourceConnectionID = req.SourceConnectionID
	}
	if req.ChunkingConfig != nil {
		chunking := value.ChunkingConfig{
			ChunkSize:    req.ChunkingConfig.ChunkSize,
			ChunkOverlap: req.ChunkingConfig.ChunkOverlap,
			Strategy:     req.ChunkingConfig.Strategy, ParentChunkSize: req.ChunkingConfig.ParentChunkSize, ChildChunkSize: req.ChunkingConfig.ChildChunkSize,
		}
		if req.ChunkingConfig.EnableParentChild != nil {
			chunking.EnableParentChild = *req.ChunkingConfig.EnableParentChild
		} else {
			chunking.EnableParentChild = true
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
	items, err := h.service.List(c.Request.Context(), authCtx.ResourceAccess())
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
	kb, err := h.service.Get(c.Request.Context(), authCtx.ResourceAccess(), id)
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
		Access: authCtx.ResourceAccess(), IsAPIKey: authCtx.IsAPIKey(),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}
