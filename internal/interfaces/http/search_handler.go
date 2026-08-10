package http

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchHTTPService is the Workspace-scoped evidence retrieval contract.
type SearchHTTPService interface {
	Search(context.Context, service.SearchInput) (*dto.SearchResponse, error)
}

// MultiSearchHTTPService is the multi-KnowledgeBase evidence retrieval contract.
type MultiSearchHTTPService interface {
	Search(context.Context, service.MultiKnowledgeSearchInput) (*dto.SearchResponse, error)
}

// SearchReplayHTTPService 是管理员固定快照回放契约。
type SearchReplayHTTPService interface {
	Replay(context.Context, service.ReplaySearchInput) (*dto.SearchResponse, error)
}

type searchHandler struct {
	service        SearchHTTPService
	multiSearchSvc MultiSearchHTTPService
	replaySvc      SearchReplayHTTPService
}

type searchRequest struct {
	Query       string `json:"query"`
	VectorTopK  *int   `json:"vector_top_k"`
	KeywordTopK *int   `json:"keyword_top_k"`
	FinalTopK   *int   `json:"final_top_k"`
}

// writeRunHeaders 把 SearchRun 响应头写入 HTTP 响应。
// 单库 body 继续返回数组；运行级元数据通过 Header 承载。
func writeRunHeaders(c *gin.Context, run dto.SearchRunSummary) {
	c.Header("X-Search-ID", run.SearchID.String())
	c.Header("X-Retrieval-Status", string(run.RetrievalStatus))
	genIDs := make([]string, 0, len(run.GenerationSnapshots))
	for _, snap := range run.GenerationSnapshots {
		genIDs = append(genIDs, snap.GenerationID.String())
	}
	c.Header("X-Generation-IDs", strings.Join(genIDs, ","))
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
	response, err := h.service.Search(c.Request.Context(), service.SearchInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, Query: request.Query,
		VectorTopK: request.VectorTopK, KeywordTopK: request.KeywordTopK, FinalTopK: request.FinalTopK,
	})
	// SearchRun 创建后的错误也先写 Header，再映射原领域错误。
	if response != nil {
		writeRunHeaders(c, response.Run)
	}
	if err != nil {
		writeServiceError(c, err)
		return
	}
	results := []*dto.SearchResult{}
	if response != nil {
		results = response.Results
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
	SearchID                string              `json:"search_id"`
	RequestedScope          string              `json:"requested_scope"`
	EffectiveScope          string              `json:"effective_scope"`
	RetrievalStatus         string              `json:"retrieval_status"`
	GenerationIDs           []uuid.UUID         `json:"generation_ids"`
	SearchedKnowledgeBaseIDs []uuid.UUID        `json:"searched_knowledge_base_ids"`
	Results                 []*dto.SearchResult `json:"results"`
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
	response, err := h.multiSearchSvc.Search(c.Request.Context(), service.MultiKnowledgeSearchInput{
		WorkspaceID: authCtx.WorkspaceID, Access: authCtx.ResourceAccess(),
		KnowledgeBaseIDs: request.KnowledgeBaseIDs, Query: request.Query,
		VectorTopK: request.VectorTopK, KeywordTopK: request.KeywordTopK, FinalTopK: request.FinalTopK,
		RequestedScope: value.SearchScopeSelected,
	})
	if response != nil {
		writeRunHeaders(c, response.Run)
	}
	if err != nil {
		writeServiceError(c, err)
		return
	}
	results := []*dto.SearchResult{}
	searchedKBIDs := request.KnowledgeBaseIDs
	if response != nil {
		results = response.Results
		if len(response.Run.EffectiveKnowledgeBaseIDs) > 0 {
			searchedKBIDs = response.Run.EffectiveKnowledgeBaseIDs
		}
	}
	genIDs := []uuid.UUID{}
	if response != nil {
		for _, snap := range response.Run.GenerationSnapshots {
			genIDs = append(genIDs, snap.GenerationID)
		}
	}
	status := ""
	requestedScope := ""
	effectiveScope := ""
	searchID := ""
	if response != nil {
		status = string(response.Run.RetrievalStatus)
		requestedScope = string(response.Run.RequestedScope)
		effectiveScope = string(response.Run.EffectiveScope)
		searchID = response.Run.SearchID.String()
	}
	c.JSON(stdhttp.StatusOK, multiSearchResponse{
		SearchID: searchID, RequestedScope: requestedScope, EffectiveScope: effectiveScope,
		RetrievalStatus: status, GenerationIDs: genIDs,
		SearchedKnowledgeBaseIDs: searchedKBIDs, Results: results,
	})
}

// searchReplayRequest 是管理员回放请求体。
type searchReplayRequest struct {
	Query string `json:"query"`
}

// replayHandler 只接收 query，用原 SearchRun 记录的固定快照重放。
func (h searchHandler) replayHandler(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	if h.replaySvc == nil {
		writeError(c, stdhttp.StatusNotImplemented, "not_implemented", "检索回放未启用")
		return
	}
	// Bearer API Key 不可调用回放。
	if authCtx.IsAPIKey() {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "API Key 不可调用检索回放")
		return
	}
	searchRunID, ok := parseUUIDParam(c, "search_id")
	if !ok {
		return
	}
	var request searchReplayRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "回放参数无效")
		return
	}
	response, err := h.replaySvc.Replay(c.Request.Context(), service.ReplaySearchInput{
		WorkspaceID: authCtx.WorkspaceID, SearchRunID: searchRunID,
		Query: request.Query, ActorRole: authCtx.Role, IsAPIKey: authCtx.IsAPIKey(),
	})
	if response != nil {
		writeRunHeaders(c, response.Run)
	}
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, response)
}

var _ = fmt.Sprintf
