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
	// ListTree 递归遍历同步根下的整棵目录树，返回外部节点。
	ListTree(ctx context.Context, conn model.SourceConnection, root model.SyncRoot) ([]model.ExternalNode, error)
	// Fetch 拉取单个可同步文档的内容（docx → markdown）与元数据。
	Fetch(ctx context.Context, conn model.SourceConnection, externalID string) (model.FetchedDocument, error)
	// Provider 返回该实现服务的 provider 标识（如 "feishu"）。
	Provider() string
}

var (
	// ErrSourceUnavailable 表示外部内容源不可达或鉴权失败。
	ErrSourceUnavailable = errors.New("source unavailable")
	// ErrSourceNotFound 表示外部对象不存在或无权限。
	ErrSourceNotFound = errors.New("source not found")
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
