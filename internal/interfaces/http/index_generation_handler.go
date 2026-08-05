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

// IndexGenerationHTTPService is the Generation list/create/activate contract.
type IndexGenerationHTTPService interface {
	List(context.Context, uuid.UUID, uuid.UUID) ([]*dto.IndexGeneration, error)
	Create(context.Context, service.CreateIndexGenerationInput) (*dto.IndexGeneration, error)
	Activate(context.Context, service.ActivateIndexGenerationInput) (*dto.IndexGeneration, error)
}

type indexGenerationHandler struct{ service IndexGenerationHTTPService }

type createIndexGenerationRequest struct {
	EmbeddingModelID uuid.UUID                `json:"embedding_model_id"`
	ChunkingConfig   *chunkingConfigRequest   `json:"chunking_config"`
	RetrievalConfig  *service.RetrievalConfig `json:"retrieval_config"`
	Rerank           *rerankSelectionRequest  `json:"rerank"`
}

// rerankSelectionRequest 解析三态重排输入：enabled=true 要求 model_id/candidate_top_k/failure_mode。
type rerankSelectionRequest struct {
	Enabled       *bool                   `json:"enabled"`
	ModelID       uuid.UUID               `json:"model_id"`
	CandidateTopK int                     `json:"candidate_top_k"`
	FailureMode   value.RerankFailureMode `json:"failure_mode"`
}

// toSelection 把请求转换为 service.RerankSelection；nil 表示省略（继承 base）。
func (r *rerankSelectionRequest) toSelection() *service.RerankSelection {
	if r == nil {
		return nil
	}
	return &service.RerankSelection{
		Enabled:       r.Enabled != nil && *r.Enabled,
		ModelID:       r.ModelID,
		CandidateTopK: r.CandidateTopK,
		FailureMode:   r.FailureMode,
	}
}

type activateIndexGenerationRequest struct {
	ArchiveManualEdits bool `json:"archive_manual_edits"`
}

func (h indexGenerationHandler) list(c *gin.Context) {
	authCtx, knowledgeBaseID, ok := generationRouteContext(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func (h indexGenerationHandler) create(c *gin.Context) {
	authCtx, knowledgeBaseID, ok := generationRouteContext(c)
	if !ok {
		return
	}
	if !authCtx.Role.AtLeast(value.RoleAdmin) {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	var request createIndexGenerationRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "Generation 配置无效")
		return
	}
	var chunkingConfig *value.ChunkingConfig
	if request.ChunkingConfig != nil {
		chunkingConfig = &value.ChunkingConfig{
			ChunkSize: request.ChunkingConfig.ChunkSize, ChunkOverlap: request.ChunkingConfig.ChunkOverlap, Strategy: request.ChunkingConfig.Strategy, ParentChunkSize: request.ChunkingConfig.ParentChunkSize, ChildChunkSize: request.ChunkingConfig.ChildChunkSize,
		}
		if request.ChunkingConfig.EnableParentChild != nil {
			chunkingConfig.EnableParentChild = *request.ChunkingConfig.EnableParentChild
		} else {
			chunkingConfig.EnableParentChild = true
		}
	}
	result, err := h.service.Create(c.Request.Context(), service.CreateIndexGenerationInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: request.EmbeddingModelID, ChunkingConfig: chunkingConfig,
		RetrievalConfig: request.RetrievalConfig, ActorRole: authCtx.Role,
		Rerank: request.Rerank.toSelection(),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, result)
}

func (h indexGenerationHandler) activate(c *gin.Context) {
	authCtx, knowledgeBaseID, ok := generationRouteContext(c)
	if !ok {
		return
	}
	if !authCtx.Role.AtLeast(value.RoleAdmin) {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	generationID, ok := parseUUIDParam(c, "generation_id")
	if !ok {
		return
	}
	var request activateIndexGenerationRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "Generation 激活参数无效")
		return
	}
	result, err := h.service.Activate(c.Request.Context(), service.ActivateIndexGenerationInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, GenerationID: generationID,
		ArchiveManualEdits: request.ArchiveManualEdits, ActorRole: authCtx.Role,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func generationRouteContext(c *gin.Context) (value.AuthContext, uuid.UUID, bool) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return value.AuthContext{}, uuid.Nil, false
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return value.AuthContext{}, uuid.Nil, false
	}
	return authCtx, knowledgeBaseID, true
}
