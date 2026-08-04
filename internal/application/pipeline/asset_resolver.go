// Package pipeline 的 AssetResolver 负责把 parser 产出的 Markdown 中的图片引用
// 转存为自有对象存储资产，并把 Markdown 中的引用替换为 public URL。
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	s3keys "github.com/dajee/langhuan/internal/adapters/storage/s3"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

// AssetResolution 是 AssetResolver.Resolve 的返回值。
type AssetResolution struct {
	Markdown string
	Assets   []model.Asset
	Warnings []model.ParseWarning
}

// AssetResolver 把 Markdown 中的图片引用归档到自有对象存储。
type AssetResolver struct {
	store      portstorage.AssetStore
	httpClient *http.Client
	cfg        config.AssetsStorageConfig
	newID      func() uuid.UUID
	// lineage 用于生成 storage key
	workspaceID uuid.UUID
	documentID  uuid.UUID
	revisionID  uuid.UUID
}

// NewAssetResolver 创建 AssetResolver。
// httpClient 应为 SSRF-safe client（adapters/httpclient.NewPublicHTTPSClient）。
func NewAssetResolver(
	store portstorage.AssetStore,
	httpClient *http.Client,
	cfg config.AssetsStorageConfig,
	workspaceID, documentID, revisionID uuid.UUID,
) *AssetResolver {
	return &AssetResolver{
		store:       store,
		httpClient:  httpClient,
		cfg:         cfg,
		newID:       uuid.New,
		workspaceID: workspaceID,
		documentID:  documentID,
		revisionID:  revisionID,
	}
}

// markdownImgRe 匹配 ![alt](url)
var markdownImgRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// htmlImgSrcRe 匹配 <img src="...">
var htmlImgSrcRe = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)

// Resolve 扫描 Markdown，归档所有图片引用，返回 normalized Markdown + assets + warnings。
func (r *AssetResolver) Resolve(ctx context.Context, markdown string) AssetResolution {
	result := AssetResolution{Markdown: markdown}

	// 收集所有图片引用
	refs := r.collectImageRefs(markdown)

	for _, ref := range refs {
		if len(result.Assets) >= r.cfg.MaxCountPerDocument {
			result.Warnings = append(result.Warnings, model.ParseWarning{
				Code:    "asset_count_exceeded",
				Message: fmt.Sprintf("文档超过 %d 张图片上限，剩余图片已跳过", r.cfg.MaxCountPerDocument),
			})
			break
		}

		asset, warning := r.resolveOne(ctx, ref)
		if warning != nil {
			result.Warnings = append(result.Warnings, *warning)
			continue
		}
		if asset != nil {
			// 替换 Markdown 中的引用
			result.Markdown = strings.ReplaceAll(result.Markdown, ref.original, asset.PublicURL)
			result.Assets = append(result.Assets, *asset)
		}
	}

	return result
}

// imageRef 描述一个图片引用的原始位置和来源。
type imageRef struct {
	original string // Markdown 中的原始引用字符串（用于替换）
	url      string // 图片 URL 或 data URI
	alt      string // alt 文本
}

func (r *AssetResolver) collectImageRefs(markdown string) []imageRef {
	var refs []imageRef
	seen := make(map[string]bool)

	// Markdown images
	for _, match := range markdownImgRe.FindAllStringSubmatch(markdown, -1) {
		url := strings.TrimSpace(match[2])
		if seen[url] {
			continue
		}
		seen[url] = true
		refs = append(refs, imageRef{original: match[2], url: url, alt: match[1]})
	}

	// HTML img tags
	for _, match := range htmlImgSrcRe.FindAllStringSubmatch(markdown, -1) {
		url := strings.TrimSpace(match[1])
		if seen[url] {
			continue
		}
		seen[url] = true
		refs = append(refs, imageRef{original: match[1], url: url})
	}

	return refs
}

func (r *AssetResolver) resolveOne(ctx context.Context, ref imageRef) (*model.Asset, *model.ParseWarning) {
	var data []byte
	var mimeType string
	var err error

	if strings.HasPrefix(ref.url, "data:") {
		data, mimeType, err = decodeDataURI(ref.url, r.cfg.MaxImageSizeBytes)
	} else if strings.HasPrefix(ref.url, "http://") || strings.HasPrefix(ref.url, "https://") {
		data, mimeType, err = r.downloadRemote(ctx, ref.url)
	} else {
		// zip 相对路径等不支持的引用格式——保留原引用
		return nil, &model.ParseWarning{
			Code:    "asset_unsupported_ref",
			Message: fmt.Sprintf("不支持的图片引用格式: %s", truncate(ref.url, 100)),
		}
	}

	if err != nil {
		return nil, &model.ParseWarning{
			Code:    "asset_download_failed",
			Message: fmt.Sprintf("图片下载失败: %v", err),
		}
	}

	// 校验 MIME
	if !r.isMimeAllowed(mimeType) {
		return nil, &model.ParseWarning{
			Code:    "asset_mime_rejected",
			Message: fmt.Sprintf("图片 MIME %s 不在允许列表", mimeType),
		}
	}

	// 校验大小
	if r.cfg.MaxImageSizeBytes > 0 && int64(len(data)) > r.cfg.MaxImageSizeBytes {
		return nil, &model.ParseWarning{
			Code:    "asset_too_large",
			Message: fmt.Sprintf("图片大小 %d 超过上限 %d", len(data), r.cfg.MaxImageSizeBytes),
		}
	}

	// 生成 asset ID 和 storage key
	assetID := r.newID()
	key := s3keys.AssetKey(r.workspaceID, r.documentID, r.revisionID, assetID, mimeType, "")

	// 上传
	stored, err := r.store.Put(ctx, portstorage.ObjectInput{
		Key:      key,
		MimeType: mimeType,
		Data:     data,
	})
	if err != nil {
		return nil, &model.ParseWarning{
			Code:    "asset_store_failed",
			Message: fmt.Sprintf("图片存储失败: %v", err),
		}
	}

	hash := sha256.Sum256(data)
	asset := &model.Asset{
		ID:                 assetID,
		WorkspaceID:        r.workspaceID,
		DocumentRevisionID: r.revisionID,
		DocumentID:         r.documentID,
		OriginalRef:        ref.original,
		StorageKey:         stored.Key,
		PublicURL:          stored.PublicURL,
		MimeType:           mimeType,
		SHA256:             hex.EncodeToString(hash[:]),
		SizeBytes:          int64(len(data)),
		Metadata: map[string]any{
			"alt": ref.alt,
		},
	}
	if stored.PublicURL == "" {
		// local 模式 public URL 是 key 本身，前端走代理 handler
		asset.PublicURL = stored.Key
	}
	return asset, nil
}

func (r *AssetResolver) downloadRemote(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("构建请求失败: %w", err)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// C3 修复：MaxImageSizeBytes=0 时表示不限制；>0 时多读 1 字节用于超限检测
	limit := int64(-1) // -1 = 无限制
	if r.cfg.MaxImageSizeBytes > 0 {
		limit = r.cfg.MaxImageSizeBytes + 1
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, "", fmt.Errorf("读取响应失败: %w", err)
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return data, mimeType, nil
}

func (r *AssetResolver) isMimeAllowed(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	for _, allowed := range r.cfg.AllowedMimeTypes {
		if strings.ToLower(strings.TrimSpace(allowed)) == mimeType {
			return true
		}
	}
	return false
}

// decodeDataURI 解析 data:image/png;base64,... 格式的 URI。
// maxBytes 限制解码后的最大字节数（0 表示不限制）。
func decodeDataURI(dataURI string, maxBytes int64) ([]byte, string, error) {
	// data:image/png;base64,iVBOR...
	if !strings.HasPrefix(dataURI, "data:") {
		return nil, "", fmt.Errorf("not a data URI")
	}
	commaIdx := strings.Index(dataURI, ",")
	if commaIdx < 0 {
		return nil, "", fmt.Errorf("invalid data URI: missing comma")
	}
	header := dataURI[5:commaIdx] // image/png;base64
	dataPart := dataURI[commaIdx+1:]

	// M3 修复：在解码前检查 base64 字符串长度，防止内存炸弹
	// base64 编码后长度约为原始数据的 4/3，用 maxBytes*1.34 作为近似上限
	if maxBytes > 0 && int64(len(dataPart)) > maxBytes*4/3+10 {
		return nil, "", fmt.Errorf("data URI 超过最大允许大小 %d 字节", maxBytes)
	}

	semiIdx := strings.Index(header, ";")
	mimeType := header
	encoding := "base64"
	if semiIdx >= 0 {
		mimeType = header[:semiIdx]
		encoding = header[semiIdx+1:]
	}

	if encoding != "base64" {
		return nil, "", fmt.Errorf("unsupported data URI encoding: %s", encoding)
	}

	data, err := base64.StdEncoding.DecodeString(dataPart)
	if err != nil {
		return nil, "", fmt.Errorf("base64 解码失败: %w", err)
	}
	return data, mimeType, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
