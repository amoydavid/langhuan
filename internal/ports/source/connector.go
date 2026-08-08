// Package source 定义外部内容源（如飞书云文档/知识库）的领域端口。
package source

import (
	"context"
	"errors"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
)

// SourceConnector 抽象一类外部内容源（飞书、Notion 等）的列举与拉取能力。
// 实现位于 internal/adapters/source/<provider>。
type SourceConnector interface {
	// ListTree 递归遍历同步根下的整棵目录树，返回快照（节点 + 完整性标记）。
	ListTree(ctx context.Context, conn model.SourceConnection, root model.SyncRoot) (TreeSnapshot, error)
	// Fetch 按上限拉取单个可同步文档的内容（docx → markdown）与元数据。
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

// FetchOptions 控制 Fetch 的拉取行为。
//
//   - MaxContentBytes 限制单次拉取的内容字节数，超过应返回 ErrSourceContentTooLarge。
//     <=0 表示不限。
type FetchOptions struct {
	MaxContentBytes int64
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
)

// EditTimeZero 表示飞书节点没有编辑时间信息时的零值。
var EditTimeZero time.Time
