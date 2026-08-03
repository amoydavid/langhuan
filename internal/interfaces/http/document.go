package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

const defaultMaxFileSizeBytes int64 = 50 * 1024 * 1024

type DocumentIngestService interface {
	Ingest(ctx context.Context, input service.IngestDocumentInput) (*service.IngestDocumentResult, error)
}

type DocumentQueryService interface {
	Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Document, error)
	List(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) ([]*dto.Document, error)
	Delete(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) error
}

type documentHandler struct {
	ingestService    DocumentIngestService
	queryService     DocumentQueryService
	maxFileSizeBytes int64
}

func (h documentHandler) list(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	items, err := h.queryService.List(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if items == nil {
		items = make([]*dto.Document, 0)
	}
	c.JSON(stdhttp.StatusOK, items)
}

func (h documentHandler) ingest(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	workspaceID := authCtx.WorkspaceID

	limit := h.maxFileSizeBytes
	if limit <= 0 {
		limit = defaultMaxFileSizeBytes
	}
	c.Request.Body = stdhttp.MaxBytesReader(c.Writer, c.Request.Body, limit+1)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		var maxBytesErr *stdhttp.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(c, stdhttp.StatusRequestEntityTooLarge, "validation_error", "file 超过大小限制")
			return
		}
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "file 不能为空")
		return
	}
	defer file.Close()

	dedupe, err := parseOptionalBool(c.Query("dedupe"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "dedupe 必须是有效布尔值")
		return
	}
	if field := unsupportedDocumentFormField(c); field != "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "不支持的表单字段: "+field)
		return
	}

	title := strings.TrimSpace(c.PostForm("title"))
	fileName := strings.TrimSpace(header.Filename)
	if title == "" {
		title = fileName
	}
	var parentNodeID *uuid.UUID
	if rawParentID := strings.TrimSpace(c.PostForm("parent_node_id")); rawParentID != "" {
		parsed, err := uuid.Parse(rawParentID)
		if err != nil {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "parent_node_id 必须是有效 UUID")
			return
		}
		parentNodeID = &parsed
	}
	input := service.IngestDocumentInput{
		WorkspaceID:     workspaceID,
		KnowledgeBaseID: knowledgeBaseID,
		Title:           title,
		FileName:        fileName,
		ContentType:     strings.TrimSpace(header.Header.Get("Content-Type")),
		SourceType:      strings.TrimSpace(c.PostForm("source_type")),
		Reader:          file,
		SizeBytes:       header.Size,
		Dedupe:          dedupe,
		ParentNodeID:    parentNodeID,
		NodeName:        strings.TrimSpace(c.PostForm("node_name")),
	}
	result, err := h.ingestService.Ingest(c.Request.Context(), input)
	if err != nil {
		if isRequestTooLarge(err) {
			writeError(c, stdhttp.StatusRequestEntityTooLarge, "validation_error", "file 超过大小限制")
			return
		}
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, result)
}

func (h documentHandler) get(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	id, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	doc, err := h.queryService.Get(c.Request.Context(), authCtx.ResourceAccess(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, doc)
}

func (h documentHandler) delete(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	documentID, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
		return
	}
	if err := h.queryService.Delete(c.Request.Context(), authCtx.ResourceAccess(), documentID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func isRequestTooLarge(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var maxBytesErr *stdhttp.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func parseOptionalBool(raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func unsupportedDocumentFormField(c *gin.Context) string {
	if c.Request.MultipartForm == nil {
		return ""
	}
	for field := range c.Request.MultipartForm.Value {
		if field != "title" && field != "source_type" && field != "parent_node_id" && field != "node_name" {
			return field
		}
	}
	for field := range c.Request.MultipartForm.File {
		if field != "file" {
			return field
		}
	}
	return ""
}
