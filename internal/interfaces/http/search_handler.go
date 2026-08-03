package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
)

// SearchHTTPService is the Workspace-scoped evidence retrieval contract.
type SearchHTTPService interface {
	Search(context.Context, service.SearchInput) ([]*dto.SearchResult, error)
}

// MultiSearchHTTPService is the multi-KnowledgeBase evidence retrieval contract.
type MultiSearchHTTPService interface {
	Search(context.Context, service.MultiKnowledgeSearchInput) ([]*dto.SearchResult, error)
}

type searchHandler struct {
	service        SearchHTTPService
	multiSearchSvc MultiSearchHTTPService
}

type searchRequest struct {
	Query       string `json:"query"`
	VectorTopK  *int   `json:"vector_top_k"`
	KeywordTopK *int   `json:"keyword_top_k"`
	FinalTopK   *int   `json:"final_top_k"`
}

func (h searchHandler) search(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	var request searchRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "检索参数无效")
		return
	}
	results, err := h.service.Search(c.Request.Context(), service.SearchInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, Query: request.Query,
		VectorTopK: request.VectorTopK, KeywordTopK: request.KeywordTopK, FinalTopK: request.FinalTopK,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, results)
}

type multiSearchRequest struct {
	KnowledgeBaseIDs []uuid.UUID `json:"knowledge_base_ids"`
	Query            string      `json:"query"`
	VectorTopK       *int        `json:"vector_top_k"`
	KeywordTopK      *int        `json:"keyword_top_k"`
	FinalTopK        *int        `json:"final_top_k"`
}

type multiSearchResponse struct {
	SearchedKnowledgeBaseIDs []uuid.UUID         `json:"searched_knowledge_base_ids"`
	Results                  []*dto.SearchResult `json:"results"`
}

// multiSearchHandler 检索多个知识库，按 Embedding 模型快照分组复用 query embedding，
// 统一 RRF 合并后返回带知识库来源的结果。
func (h searchHandler) multiSearchHandler(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	if h.multiSearchSvc == nil {
		writeError(c, stdhttp.StatusNotImplemented, "not_implemented", "多知识库检索未启用")
		return
	}
	var request multiSearchRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "检索参数无效")
		return
	}
	results, err := h.multiSearchSvc.Search(c.Request.Context(), service.MultiKnowledgeSearchInput{
		WorkspaceID: authCtx.WorkspaceID, Access: authCtx.ResourceAccess(),
		KnowledgeBaseIDs: request.KnowledgeBaseIDs, Query: request.Query,
		VectorTopK: request.VectorTopK, KeywordTopK: request.KeywordTopK, FinalTopK: request.FinalTopK,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if results == nil {
		results = []*dto.SearchResult{}
	}
	c.JSON(stdhttp.StatusOK, multiSearchResponse{
		SearchedKnowledgeBaseIDs: request.KnowledgeBaseIDs, Results: results,
	})
}
