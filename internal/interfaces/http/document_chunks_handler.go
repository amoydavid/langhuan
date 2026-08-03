package http

import (
	"context"
	stdhttp "net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
)

// DocumentChunksHTTPService lists one Document's current effective ChunkSet.
type DocumentChunksHTTPService interface {
	List(context.Context, service.DocumentChunksInput) (*dto.DocumentChunkPage, error)
}

type documentChunksHandler struct {
	service DocumentChunksHTTPService
}

func (h documentChunksHandler) list(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	documentID, ok := parseUUIDParam(c, "document_id")
	if !ok {
		return
	}
	filter, ok := parseDocumentChunksFilter(c)
	if !ok {
		return
	}
	filter.WorkspaceID = authCtx.WorkspaceID
	filter.KnowledgeBaseID = knowledgeBaseID
	filter.DocumentID = documentID
	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func parseDocumentChunksFilter(c *gin.Context) (service.DocumentChunksInput, bool) {
	input := service.DocumentChunksInput{Cursor: strings.TrimSpace(c.Query("cursor"))}
	if rawEnabled, exists := c.GetQuery("enabled"); exists {
		rawEnabled = strings.TrimSpace(rawEnabled)
		if rawEnabled != "true" && rawEnabled != "false" {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "enabled 必须是 true 或 false")
			return service.DocumentChunksInput{}, false
		}
		enabled := rawEnabled == "true"
		input.Enabled = &enabled
	}
	if rawLimit, exists := c.GetQuery("limit"); exists {
		limit, err := strconv.Atoi(strings.TrimSpace(rawLimit))
		if err != nil || limit < 1 || limit > 200 {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "limit 必须是 1 到 200 的整数")
			return service.DocumentChunksInput{}, false
		}
		input.Limit = limit
	}
	return input, true
}
