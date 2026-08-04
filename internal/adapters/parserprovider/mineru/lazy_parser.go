package mineru

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
	storageport "github.com/dajee/langhuan/internal/ports/storage"
)

// CredentialSelector 是 ParserProviderSelector 的最小接口，避免循环依赖。
type CredentialSelector interface {
	SelectMinerU(ctx context.Context, workspaceID uuid.UUID) (SelectedCredential, error)
}

// SelectedCredential 携带选中的 MinerU provider 及解密后的凭据。
type SelectedCredential struct {
	ProviderID      uuid.UUID
	Config          map[string]any
	CredentialsJSON []byte
}

// LazyParser 在 Start 时延迟解析 MinerU 凭据，然后委托给实际 Parser。
// 它实现 DocumentParser + AsyncDocumentParser。
type LazyParser struct {
	selector  CredentialSelector
	rawStore  storageport.RawDocumentStore
	cfg       LazyParserConfig
}

// LazyParserConfig 是 LazyParser 的运行参数（来自 config.yaml 的 mineru 块）。
type LazyParserConfig struct {
	ModelVersion              string
	PollInterval              time.Duration
	MaxPollAttempts           int
	UploadTimeout             time.Duration
	ResultDownloadTimeout     time.Duration
}

// NewLazyParser 创建延迟凭据解析的 MinerU parser。
func NewLazyParser(selector CredentialSelector, rawStore storageport.RawDocumentStore, cfg LazyParserConfig) *LazyParser {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Second
	}
	if cfg.MaxPollAttempts == 0 {
		cfg.MaxPollAttempts = 180
	}
	if cfg.UploadTimeout == 0 {
		cfg.UploadTimeout = 120 * time.Second
	}
	if cfg.ResultDownloadTimeout == 0 {
		cfg.ResultDownloadTimeout = 120 * time.Second
	}
	return &LazyParser{selector: selector, rawStore: rawStore, cfg: cfg}
}

// Supports 只支持 PDF。
func (p *LazyParser) Supports(fileType string) bool {
	return strings.ToLower(fileType) == "pdf"
}

// Parse 同步路径不可用。
func (p *LazyParser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	return nil, fmt.Errorf("%w: MinerU PDF parser 只支持异步解析", parserport.ErrUnsupportedFileType)
}

// Start 实现 AsyncDocumentParser.Start。
func (p *LazyParser) Start(ctx context.Context, input parserport.AsyncParseInput) (*parserport.AsyncParseStart, error) {
	inner, err := p.buildInner(ctx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return inner.Start(ctx, input)
}

// Poll 实现 AsyncDocumentParser.Poll。
func (p *LazyParser) Poll(ctx context.Context, input parserport.AsyncParsePollInput) (*parserport.AsyncParsePollResult, error) {
	inner, err := p.buildInner(ctx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return inner.Poll(ctx, input)
}

// buildInner 解析凭据并构造实际 Parser。
func (p *LazyParser) buildInner(ctx context.Context, workspaceID uuid.UUID) (*Parser, error) {
	cred, err := p.selector.SelectMinerU(ctx, workspaceID)
	if err != nil {
		// M6 修复：不包装为 ErrMissingParserProvider（permanent error），
		// 让原始错误传播——DB 错误等瞬时故障应可重试，
		// "没有可用的 MinerU Provider" 才是 permanent（由 selector 层自行返回）
		return nil, err
	}

	// 从 config 和 credentials 构造 MinerU client
	baseURL := "https://mineru.net"
	modelVersion := p.cfg.ModelVersion
	if v, ok := cred.Config["base_url"].(string); ok && v != "" {
		baseURL = v
	}
	if v, ok := cred.Config["model_version"].(string); ok && v != "" {
		modelVersion = v
	}

	var creds mineruCredentials
	if err := json.Unmarshal(cred.CredentialsJSON, &creds); err != nil {
		return nil, fmt.Errorf("解码 MinerU 凭据失败: %w", err)
	}

	client := NewClient(ClientConfig{
		BaseURL:      baseURL,
		Token:        creds.Token,
		ModelVersion: modelVersion,
		HTTPTimeout:  p.cfg.UploadTimeout,
	})

	return NewParser(client, p.rawStore, ParserConfig{
		ModelVersion:          modelVersion,
		PollInterval:          p.cfg.PollInterval,
		MaxPollAttempts:       p.cfg.MaxPollAttempts,
		UploadTimeout:         p.cfg.UploadTimeout,
		ResultDownloadTimeout: p.cfg.ResultDownloadTimeout,
	}), nil
}

// 确保 LazyParser 实现接口
var (
	_ parserport.DocumentParser      = (*LazyParser)(nil)
	_ parserport.AsyncDocumentParser = (*LazyParser)(nil)
)
