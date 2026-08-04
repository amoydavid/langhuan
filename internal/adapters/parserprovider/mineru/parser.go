package mineru

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
	storageport "github.com/dajee/langhuan/internal/ports/storage"
)

// MinerUClient 是 MinerU client 的最小接口，便于测试注入 fake。
type MinerUClient interface {
	RequestUploadURL(ctx context.Context, fileName string, sizeBytes int64) (*UploadTicket, error)
	Upload(ctx context.Context, uploadURL string, content io.Reader, contentType string) error
	Poll(ctx context.Context, batchID string) (*TaskResult, error)
	Download(ctx context.Context, resultURL string) (markdown string, rawBytes []byte, err error)
}

// Parser 实现 parserport.DocumentParser 和 parserport.AsyncDocumentParser。
type Parser struct {
	client    MinerUClient
	rawStore  storageport.RawDocumentStore
	config    ParserConfig
}

// ParserConfig 是 MinerU parser 的运行参数。
type ParserConfig struct {
	ModelVersion              string
	PollInterval              time.Duration
	MaxPollAttempts           int
	UploadTimeout             time.Duration
	ResultDownloadTimeout     time.Duration
}

// 确保 *Client 实现 MinerUClient 接口
var _ MinerUClient = (*Client)(nil)
func NewParser(client MinerUClient, rawStore storageport.RawDocumentStore, config ParserConfig) *Parser {
	if config.PollInterval == 0 {
		config.PollInterval = 10 * time.Second
	}
	if config.MaxPollAttempts == 0 {
		config.MaxPollAttempts = 180
	}
	return &Parser{client: client, rawStore: rawStore, config: config}
}

// Supports 只支持 PDF。
func (p *Parser) Supports(fileType string) bool {
	return strings.ToLower(fileType) == "pdf"
}

// Parse 同步解析路径——MinerU 是异步 parser，同步 Parse 返回错误。
// worker 层应通过类型断言检测 AsyncDocumentParser 走异步路径。
func (p *Parser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	return nil, fmt.Errorf("%w: MinerU PDF parser 只支持异步解析", parserport.ErrUnsupportedFileType)
}

// Start 实现 AsyncDocumentParser.Start：读取 raw PDF → 上传 MinerU → 返回 batch_id。
func (p *Parser) Start(ctx context.Context, input parserport.AsyncParseInput) (*parserport.AsyncParseStart, error) {
	if !p.Supports(input.FileType) {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, input.FileType)
	}

	// 从存储读取 raw PDF
	reader, err := p.rawStore.Open(ctx, input.RawStorageKey)
	if err != nil {
		return nil, fmt.Errorf("读取原始 PDF 失败: %w", err)
	}
	defer reader.Close()

	// 申请上传地址
	ticket, err := p.client.RequestUploadURL(ctx, "document.pdf", 0)
	if err != nil {
		return nil, fmt.Errorf("申请 MinerU 上传地址失败: %w", err)
	}

	// 上传 PDF 到 MinerU 签名 URL
	uploadURL := ""
	if len(ticket.URLs) > 0 {
		uploadURL = ticket.URLs[0]
	}
	if uploadURL == "" {
		return nil, fmt.Errorf("MinerU 未返回上传地址")
	}
	if err := p.client.Upload(ctx, uploadURL, reader, "application/pdf"); err != nil {
		return nil, fmt.Errorf("上传 PDF 到 MinerU 失败: %w", err)
	}

	return &parserport.AsyncParseStart{
		ExternalJobID: ticket.BatchID,
		Status:        parserport.AsyncSubmitted,
		Payload: map[string]any{
			"batch_id":      ticket.BatchID,
			"model_version": p.config.ModelVersion,
			"poll_count":    0,
		},
	}, nil
}

// Poll 实现 AsyncDocumentParser.Poll：轮询 MinerU → 成功则下载 Markdown 并组装 ParsedDocument。
func (p *Parser) Poll(ctx context.Context, input parserport.AsyncParsePollInput) (*parserport.AsyncParsePollResult, error) {
	batchID := input.ExternalJobID
	if batchID == "" {
		if v, ok := input.Payload["batch_id"].(string); ok {
			batchID = v
		}
	}
	if batchID == "" {
		return nil, fmt.Errorf("MinerU Poll 缺少 batch_id")
	}

	result, err := p.client.Poll(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("MinerU 轮询失败: %w", err)
	}

	payload := input.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	switch result.Status {
	case TaskStatusRunning:
		payload["poll_count"] = incrementPollCount(payload)
		return &parserport.AsyncParsePollResult{
			Status:     parserport.AsyncRunning,
			Payload:    payload,
			RetryAfter: p.config.PollInterval,
		}, nil

	case TaskStatusFailed:
		return &parserport.AsyncParsePollResult{
			Status:       parserport.AsyncFailed,
			ErrorCode:    result.ErrorCode,
			ErrorMessage: result.ErrorMessage,
		}, nil

	case TaskStatusSucceeded:
		if result.FullResultURL == "" {
			return &parserport.AsyncParsePollResult{
				Status:       parserport.AsyncFailed,
				ErrorCode:    "no_result_url",
				ErrorMessage: "MinerU 成功但未返回结果下载地址",
			}, nil
		}
		markdown, _, err := p.client.Download(ctx, result.FullResultURL)
		if err != nil {
			return nil, fmt.Errorf("下载 MinerU 结果失败: %w", err)
		}
		parsed, err := buildParsedDocument(markdown, p.config.ModelVersion)
		if err != nil {
			return nil, err
		}
		return &parserport.AsyncParsePollResult{
			Status:   parserport.AsyncSucceeded,
			Document: parsed,
			Payload:  payload,
		}, nil

	default:
		return &parserport.AsyncParsePollResult{
			Status:     parserport.AsyncRunning,
			Payload:    payload,
			RetryAfter: p.config.PollInterval,
		}, nil
	}
}

func incrementPollCount(payload map[string]any) int {
	if v, ok := payload["poll_count"]; ok {
		switch n := v.(type) {
		case int:
			return n + 1
		case int64:
			return int(n) + 1
		case float64:
			return int(n) + 1
		}
	}
	return 1
}
