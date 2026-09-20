// Package source 定义外部内容源（如飞书云文档/知识库）的领域端口。
package source

import (
	"context"
	"errors"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
)

// SourceConnector 抽象一类外部内容源（飞书、网站等）的列举与拉取能力。
// 实现位于 internal/adapters/source/<provider>。
type SourceConnector interface {
	// ListTree 递归遍历同步根下的整棵目录树，返回快照（节点 + 完整性标记）。
	ListTree(ctx context.Context, conn model.SourceConnection, root model.SyncRoot, options ListOptions) (TreeSnapshot, error)
	// Fetch 按上限拉取单个可同步文档的内容（docx/web page → markdown）与元数据。
	Fetch(ctx context.Context, conn model.SourceConnection, externalID string, options FetchOptions) (model.FetchedDocument, error)
	// Provider 返回该实现服务的 provider 标识（如 "feishu"）。
	Provider() string
}

// TreeSnapshot 是一次目录树列举的结果快照。
//
//   - Nodes 为本次列举到的外部节点（含 folder 与文档）。
//   - Complete 表示本次列举是否完整；截断/限流等场景为 false，调用方据此决定是否做删除检测。
//   - Warnings 记录非致命告警（如部分子树被跳过），不携带敏感数据。
//   - MaxEditTime 是本次快照中所有节点的最大编辑时间，用于推进增量游标。
type TreeSnapshot struct {
	Nodes       []model.ExternalNode
	Complete    bool
	Warnings    []string
	MaxEditTime time.Time
}

// ListOptions 控制 ListTree 的列举行为。
//
//   - SourceConfig 携带源级 config（kb_sources.config jsonb 解码后的 map），
//     供 web 源 connector 解析模式/引擎/礼貌性参数并据此发现页面。
//     飞书连接器忽略该字段（飞书源配置在 KB 的 source_config 上，见现有同步路径）。
type ListOptions struct {
	SourceConfig map[string]any
}

// FetchOptions 控制 Fetch 的拉取行为。
//
//   - MaxContentBytes 限制单次拉取的内容字节数，超过应返回 ErrSourceContentTooLarge。
//     <=0 表示不限。
//   - SourceConfig 含义同 ListOptions.SourceConfig；凭证由 service 预解密后经
//     约定键注入（仅内存传递，不落库、不进日志），飞书忽略。
//   - IfNoneMatch / IfModifiedSince 供 http 引擎发条件请求（etag / last_modified）；
//     空值不发送，命中 304 时返回 FetchedDocument.NotModified = true。
//   - LastEngine 是该文档上次成功抓取的引擎（documents.source_meta.last_engine），
//     供 web auto 引擎做 last_engine 短路（沿用上次成功引擎，spec §6.2）；
//     空串表示无历史（新增文档/历史无记录）。
type FetchOptions struct {
	MaxContentBytes int64
	SourceConfig    map[string]any
	IfNoneMatch     string
	IfModifiedSince string
	LastEngine      string
}

var (
	// ErrSourceUnavailable 表示外部内容源不可达或鉴权失败。
	ErrSourceUnavailable = errors.New("source unavailable")
	// ErrSourceNotFound 表示外部对象不存在或无权限。
	ErrSourceNotFound = errors.New("source not found")
	// ErrSourceContentTooLarge 表示拉取的文档内容超过配置的上限。
	ErrSourceContentTooLarge = errors.New("source content too large")
)

// SyncRootKind 是同步根的类型。
type SyncRootKind = string

const (
	// SyncRootDriveFolder 表示飞书云空间文件夹。
	SyncRootDriveFolder = "drive_folder"
	// SyncRootWikiNode 表示飞书知识库节点。
	SyncRootWikiNode = "wiki_node"
	// SyncRootWeb 表示网站内容源：Token 为 seed_url（crawl）/ sitemap_url（sitemap），
	// urls 模式下为空串（URL 列表从 ListOptions.SourceConfig 读取，spec §5.5）。
	SyncRootWeb = "web"
)

// EditTimeZero 表示飞书节点没有编辑时间信息时的零值。
var EditTimeZero time.Time
