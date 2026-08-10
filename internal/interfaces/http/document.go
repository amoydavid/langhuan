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
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

const defaultMaxFileSizeBytes int64 = 50 * 1024 * 1024

type DocumentIngestService interface {
	Ingest(ctx context.Context, input service.IngestDocumentInput) (*service.IngestDocumentResult, error)
}

type DocumentQueryService interface {
	Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Document, error)
	List(ctx context.Context, filter service.DocumentListFilter) ([]*dto.Document, error)
	Delete(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) error
}

// DocumentAssetListService 提供按 Document 查询图片资产的能力。
type DocumentAssetListService interface {
	ListByDocument(ctx context.Context, workspaceID, documentID uuid.UUID) ([]*model.Asset, error)
}

// DocumentAssetGetter 提供按 workspace + asset ID 查询单个资产的能力。
type DocumentAssetGetter interface {
	GetByID(ctx context.Context, workspaceID, assetID uuid.UUID) (*model.Asset, error)
}

// AssetContentStore 按 storage key 打开资产内容，供鉴权代理 handler 返回图片。
type AssetContentStore interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// DocumentRetryService 提供失败文档/任务的幂等重试。
type DocumentRetryService interface {
	RetryDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) (*service.RetryResult, error)
	RetryJob(ctx context.Context, access value.ResourceAccess, jobID uuid.UUID) (*service.RetryResult, error)
}

type documentHandler struct {
	ingestService     DocumentIngestService
	queryService      DocumentQueryService
	assetService      DocumentAssetListService
	assetGetter       DocumentAssetGetter
	assetContentStore AssetContentStore
	retryService      DocumentRetryService
	maxFileSizeBytes  int64
}

type ingestTextDocumentRequest struct {
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	ContentType  string     `json:"content_type"`
	ParentNodeID *uuid.UUID `json:"parent_node_id"`
}

func (h documentHandler) ingestText(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	kbID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	var req ingestTextDocumentRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" || strings.TrimSpace(req.ContentType) != "markdown" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "title、content 和 content_type=markdown 为必填")
		return
	}
	content := []byte(req.Content)
	limit := h.maxFileSizeBytes
	if limit <= 0 {
		limit = defaultMaxFileSizeBytes
	}
	if int64(len(content)) > limit {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "content 超过大小限制")
		return
	}
	// Idempotency-Key (optional, Bearer-only contract). Empty key keeps the
	// legacy non-idempotent path. Validation: 1..128 ASCII bytes, no CR/LF.
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(key) > 128 || (key != "" && strings.ContainsAny(key, "\r\n")) || (key != "" && !isASCII(key)) {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "Idempotency-Key 无效")
		return
	}
	input := service.IngestDocumentInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: kbID,
		Title: strings.TrimSpace(req.Title), FileName: strings.TrimSpace(req.Title) + ".md",
		ContentType: "text/markdown", SourceType: "api", Reader: strings.NewReader(req.Content),
		SizeBytes: int64(len(content)), ParentNodeID: req.ParentNodeID, NodeName: strings.TrimSpace(req.Title),
		IdempotencyKey: key,
	}
	// Only Bearer callers anchor an idempotency row; Session callers ignore the
	// key (they are not part of the programmatic replay contract).
	if authCtx.IsAPIKey() {
		input.CallerAPIKeyID = &authCtx.PrincipalID
	}
	result, err := h.ingestService.Ingest(c.Request.Context(), input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, result)
}

// isASCII reports whether s contains only ASCII bytes (0x00-0x7F). Used for
// Idempotency-Key validation per the programmatic-access contract.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			return false
		}
	}
	return true
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
	filter := service.DocumentListFilter{WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, Access: authCtx.ResourceAccess()}
	if raw := strings.TrimSpace(c.Query("kind")); raw != "" {
		kind := value.DocumentKind(raw)
		if err := kind.Validate(); err != nil {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "kind 必须是 file、faq 或 web")
			return
		}
		filter.Kind = &kind
	}
	items, err := h.queryService.List(c.Request.Context(), filter)
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

// retryDocument 重试失败文档的最新 revision，复位 failed 状态并重新入队解析。
func (h documentHandler) retryDocument(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if h.retryService == nil {
		writeError(c, stdhttp.StatusNotImplemented, "not_implemented", "重试未启用")
		return
	}
	documentID, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
		return
	}
	result, err := h.retryService.RetryDocument(c.Request.Context(), authCtx.ResourceAccess(), documentID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, result)
}

// retryJob 重试失败 job 关联的 revision。
func (h documentHandler) retryJob(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if h.retryService == nil {
		writeError(c, stdhttp.StatusNotImplemented, "not_implemented", "重试未启用")
		return
	}
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	result, err := h.retryService.RetryJob(c.Request.Context(), authCtx.ResourceAccess(), jobID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, result)
}

// assets 返回指定 Document 当前 active revision 的图片资产列表。
func (h documentHandler) assets(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if h.assetService == nil {
		writeError(c, stdhttp.StatusNotImplemented, "not_implemented", "资产查询未启用")
		return
	}
	documentID, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
		return
	}
	assets, err := h.assetService.ListByDocument(c.Request.Context(), authCtx.WorkspaceID, documentID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	result := make([]dto.DocumentAsset, 0, len(assets))
	for _, asset := range assets {
		result = append(result, dto.DocumentAssetFromModel(asset))
	}
	c.JSON(stdhttp.StatusOK, result)
}

// assetContent 是 local 模式的资产代理 handler：鉴权后按 storage_key
// 读取图片内容返回。s3 模式资产走存储层 CDN URL，无需经过该 handler。
func (h documentHandler) assetContent(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if h.assetGetter == nil || h.assetContentStore == nil {
		writeError(c, stdhttp.StatusNotImplemented, "not_implemented", "资产内容服务未启用")
		return
	}
	documentID, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
		return
	}
	assetID, err := uuid.Parse(c.Param("asset_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "asset_id 必须是有效 UUID")
		return
	}
	asset, err := h.assetGetter.GetByID(c.Request.Context(), authCtx.WorkspaceID, assetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	// 归属校验：资产必须属于当前 document，防止跨文档访问
	if asset.DocumentID != documentID {
		writeError(c, stdhttp.StatusNotFound, "not_found", "资产不存在")
		return
	}
	reader, err := h.assetContentStore.Open(c.Request.Context(), asset.StorageKey)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	defer reader.Close()
	c.Header("Content-Type", asset.MimeType)
	c.Header("Cache-Control", "private, max-age=3600")
	c.Status(stdhttp.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
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
