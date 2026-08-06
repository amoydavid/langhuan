package http

import (
	"context"
	stdhttp "net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSummaryHTTPService is the workbench summary and activity query contract.
type KnowledgeBaseSummaryHTTPService interface {
	GetSummary(context.Context, value.ResourceAccess, uuid.UUID) (*dto.KnowledgeBaseSummary, error)
	ListJobs(context.Context, value.ResourceAccess, uuid.UUID, service.JobListFilter) (*dto.JobSummaryPage, error)
}

type knowledgeBaseSummaryHandler struct {
	service KnowledgeBaseSummaryHTTPService
}

func (h knowledgeBaseSummaryHandler) getSummary(c *gin.Context) {
	authCtx, knowledgeBaseID, ok := knowledgeBaseSummaryRouteContext(c)
	if !ok {
		return
	}
	result, err := h.service.GetSummary(c.Request.Context(), authCtx.ResourceAccess(), knowledgeBaseID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func (h knowledgeBaseSummaryHandler) listJobs(c *gin.Context) {
	authCtx, knowledgeBaseID, ok := knowledgeBaseSummaryRouteContext(c)
	if !ok {
		return
	}
	filter, ok := parseKnowledgeBaseJobListFilter(c)
	if !ok {
		return
	}
	result, err := h.service.ListJobs(c.Request.Context(), authCtx.ResourceAccess(), knowledgeBaseID, filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func knowledgeBaseSummaryRouteContext(c *gin.Context) (value.AuthContext, uuid.UUID, bool) {
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

func parseKnowledgeBaseJobListFilter(c *gin.Context) (service.JobListFilter, bool) {
	filter := service.JobListFilter{Cursor: strings.TrimSpace(c.Query("cursor"))}
	if rawDocumentID := strings.TrimSpace(c.Query("document_id")); rawDocumentID != "" {
		documentID, err := uuid.Parse(rawDocumentID)
		if err != nil {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
			return service.JobListFilter{}, false
		}
		filter.DocumentID = &documentID
	}
	if rawStatus := strings.TrimSpace(c.Query("status")); rawStatus != "" {
		status := value.JobStatus(rawStatus)
		if !validKnowledgeBaseJobQueryStatus(status) {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "status 不是支持的任务状态")
			return service.JobListFilter{}, false
		}
		filter.Status = status
	}
	if rawLimit, exists := c.GetQuery("limit"); exists {
		limit, err := strconv.Atoi(strings.TrimSpace(rawLimit))
		if err != nil || limit < 1 || limit > 100 {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "limit 必须是 1 到 100 的整数")
			return service.JobListFilter{}, false
		}
		filter.Limit = limit
	}
	return filter, true
}

func validKnowledgeBaseJobQueryStatus(status value.JobStatus) bool {
	switch status {
	case value.JobStatusPending, value.JobStatusQueued, value.JobStatusRunning,
		value.JobStatusCompleted, value.JobStatusSucceeded, value.JobStatusFailed, value.JobStatusCancelled:
		return true
	default:
		return false
	}
}
