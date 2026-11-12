package http

import (
	"context"
	"errors"
	"fmt"
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
	// SetAIUseEnabled 切换文档级 AI 政策（C02）。
	SetAIUseEnabled(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID, enabled bool) error
}

// DocumentAssetListService 提供按 Document 查询图片资产的能力。
type DocumentAssetListService interface {
	ListByDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) ([]*model.Asset, error)
}

// DocumentAssetGetter 提供按 workspace + asset ID 查询单个资产的能力。
type DocumentAssetGetter interface {
	GetByID(ctx context.Context, access value.ResourceAccess, assetID uuid.UUID) (*model.Asset, error)
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
		IdempotencyKey: key, Access: authCtx.ResourceAccess(),
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
	// kb_source_id：按产生文档的 web 内容源过滤（v1.3.0 源详情页，spec §9.4）。
	if raw := strings.TrimSpace(c.Query("kb_source_id")); raw != "" {
		sourceID, err := uuid.Parse(raw)
		if err != nil {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "kb_source_id 必须是有效 UUID")
			return
		}
		filter.KbSourceID = &sourceID
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
		Access:          authCtx.ResourceAccess(),
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

// patchDocumentRequest 是文档策略更新的请求体（C02）。字段按需扩展。
type patchDocumentRequest struct {
	// AIUseEnabled 为 false 时该文档不得用于新检索（召回前排除）；
	// 原文阅读与资产访问不受影响。
	AIUseEnabled *bool `json:"ai_use_enabled"`
}

// patchDocument 更新文档策略字段（当前仅 ai_use_enabled）。documents:write。
func (h documentHandler) patchDocument(c *gin.Context) {
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
	var request patchDocumentRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if request.AIUseEnabled == nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "ai_use_enabled 不能为空")
		return
	}
	if err := h.queryService.SetAIUseEnabled(
		c.Request.Context(), authCtx.ResourceAccess(), documentID, *request.AIUseEnabled,
	); err != nil {
		writeServiceError(c, err)
		return
	}
	document, err := h.queryService.Get(c.Request.Context(), authCtx.ResourceAccess(), documentID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, document)
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
	assets, err := h.assetService.ListByDocument(c.Request.Context(), authCtx.ResourceAccess(), documentID)
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
	asset, err := h.assetGetter.GetByID(c.Request.Context(), authCtx.ResourceAccess(), assetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	// 归属校验：资产必须属于当前 document，防止跨文档访问
	if asset.DocumentID != documentID {
		writeError(c, stdhttp.StatusNotFound, "not_found", "资产不存在")
		return
	}
	// C05：ETag 以资产内容哈希标识（不可变存储键内容不变），支持条件请求。
	etag := `"` + asset.SHA256 + `"`
	c.Header("ETag", etag)
	c.Header("Accept-Ranges", "bytes")
	if match := c.GetHeader("If-None-Match"); match != "" && etagMatch(match, etag) {
		c.Status(stdhttp.StatusNotModified)
		return
	}
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		start, end, ok := parseByteRange(rangeHeader, asset.SizeBytes)
		if !ok {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", asset.SizeBytes))
			writeError(c, stdhttp.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable", "Range 不可满足")
			return
		}
		reader, err := h.assetContentStore.Open(c.Request.Context(), asset.StorageKey)
		if err != nil {
			writeServiceError(c, err)
			return
		}
		defer reader.Close()
		// 起始偏移统一以丢弃读取实现（存储层 Open 不承诺 Seekable）。
		if start > 0 {
			if _, err := io.CopyN(io.Discard, reader, int64(start)); err != nil {
				writeServiceError(c, err)
				return
			}
		}
		length := int64(end - start + 1)
		c.Header("Content-Type", asset.MimeType)
		c.Header("Cache-Control", "private, max-age=3600")
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, asset.SizeBytes))
		c.Status(stdhttp.StatusPartialContent)
		_, _ = io.CopyN(c.Writer, reader, length)
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

// parseByteRange 解析单区间 bytes=start-end（end 可省略或超出 size，
// 按规范夹紧）。多区间/非法格式返回 false（调用方回退整段或 416）。
func parseByteRange(header string, size int64) (int, int, bool) {
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	startStr, endStr, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	start, err := strconv.Atoi(strings.TrimSpace(startStr))
	if err != nil || start < 0 {
		return 0, 0, false
	}
	if size <= 0 || start >= int(size) {
		return 0, 0, false
	}
	end := int(size) - 1
	if trimmed := strings.TrimSpace(endStr); trimmed != "" {
		parsed, err := strconv.Atoi(trimmed)
		if err != nil || parsed < start {
			return 0, 0, false
		}
		if parsed < end {
			end = parsed
		}
	}
	return start, end, true
}

// etagMatch 判断 If-None-Match 是否命中当前 ETag（支持 * 与逗号分隔列表，
// 弱比较 W/ 前缀按强比较处理——资产内容不可变，语义等价）。
func etagMatch(header, etag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
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
