package parser

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

var (
	ErrUnsupportedFileType = errors.New("unsupported file type")
	ErrInvalidEncoding     = errors.New("invalid encoding")
	ErrInvalidDocument     = errors.New("invalid document")
	ErrEmptyDocument       = errors.New("empty document")
	ErrParseLimitExceeded  = errors.New("parse limit exceeded")
	ErrAsyncParseFailed    = errors.New("async parse failed")
	ErrMissingParserProvider = errors.New("missing parser provider credential")
)

type ParseInput struct {
	FileType string
	Title    string
	Content  []byte
	Metadata map[string]any
}

type ParsedDocument struct {
	Markdown string
	Manifest model.ParseManifest
}

type DocumentParser interface {
	Parse(ctx context.Context, input ParseInput) (*ParsedDocument, error)
	Supports(fileType string) bool
}

// AsyncStatus 描述异步解析任务的状态。
type AsyncStatus string

const (
	AsyncSubmitted AsyncStatus = "submitted"
	AsyncRunning   AsyncStatus = "running"
	AsyncSucceeded AsyncStatus = "succeeded"
	AsyncFailed    AsyncStatus = "failed"
)

// AsyncParseInput 是异步解析器的提交输入，包含原始文件存储 key 和 lineage。
type AsyncParseInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	DocumentID      uuid.UUID
	RevisionID      uuid.UUID
	JobID           uuid.UUID
	FileType        string
	Title           string
	RawStorageKey   string
	ContentType     string
	Metadata        map[string]any
}

// AsyncParseStart 是 Start 的返回值，包含外部任务标识和待持久化的 payload。
type AsyncParseStart struct {
	ExternalJobID string
	Payload       map[string]any
	Status        AsyncStatus
}

// AsyncParsePollInput 是 Poll 的输入，携带上次 Start/Poll 返回的外部标识与 payload。
type AsyncParsePollInput struct {
	AsyncParseInput
	ExternalJobID string
	Payload       map[string]any
}

// AsyncParsePollResult 是 Poll 的返回值。
type AsyncParsePollResult struct {
	Status       AsyncStatus
	Document     *ParsedDocument // succeeded 时填充
	Payload      map[string]any  // 更新到 jobs.payload
	RetryAfter   time.Duration   // running 时的下次轮询间隔
	ErrorCode    string          // failed 时
	ErrorMessage string          // failed 时
}

// AsyncDocumentParser 是可选的异步 parser 能力。
// worker 层 parse_start/parse_poll 通过类型断言检测 parser 是否实现该接口；
// 不实现则继续走同步 Parse 一次性完成。
type AsyncDocumentParser interface {
	Start(ctx context.Context, input AsyncParseInput) (*AsyncParseStart, error)
	Poll(ctx context.Context, input AsyncParsePollInput) (*AsyncParsePollResult, error)
}
