package mineru

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
	storageport "github.com/dajee/langhuan/internal/ports/storage"
)

// fakeMinerUClient 实现 MinerUClient 用于测试。
type fakeMinerUClient struct {
	uploadTicket *UploadTicket
	uploadURL    string
	pollResult   *TaskResult
	downloadMD   string
	downloadRaw  []byte // 非空时替代 downloadMD 作为 Download 的 rawBytes
	downloadErr  error
	pollErr      error
	uploadErr    error
	requestErr   error
}

func (f *fakeMinerUClient) RequestUploadURL(ctx context.Context, fileName string, sizeBytes int64) (*UploadTicket, error) {
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	if f.uploadTicket != nil {
		return f.uploadTicket, nil
	}
	return &UploadTicket{BatchID: "batch-123", URLs: []string{"https://upload.example.com/signed"}}, nil
}

func (f *fakeMinerUClient) Upload(ctx context.Context, uploadURL string, content io.Reader, contentType string) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	f.uploadURL = uploadURL
	return nil
}

func (f *fakeMinerUClient) Poll(ctx context.Context, batchID string) (*TaskResult, error) {
	if f.pollErr != nil {
		return nil, f.pollErr
	}
	if f.pollResult != nil {
		return f.pollResult, nil
	}
	return &TaskResult{Status: TaskStatusSucceeded, FullResultURL: "https://result.example.com/full.zip"}, nil
}

func (f *fakeMinerUClient) Download(ctx context.Context, resultURL string) (string, []byte, error) {
	if f.downloadErr != nil {
		return "", nil, f.downloadErr
	}
	if f.downloadRaw != nil {
		return f.downloadMD, f.downloadRaw, nil
	}
	if f.downloadMD != "" {
		return f.downloadMD, []byte(f.downloadMD), nil
	}
	return "# 测试标题\n\n段落内容", []byte("# 测试标题\n\n段落内容"), nil
}

// fakeRawStore 实现 storageport.RawDocumentStore 用于测试。
type fakeRawStore struct {
	content []byte
	key     string
}

func (s *fakeRawStore) Put(ctx context.Context, input storageport.RawDocumentInput) (*storageport.RawDocumentObject, error) {
	return nil, nil
}
func (s *fakeRawStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.key != "" && s.key != key {
		return nil, errors.New("key not found")
	}
	return io.NopCloser(bytes.NewReader(s.content)), nil
}
func (s *fakeRawStore) Delete(ctx context.Context, key string) error { return nil }

func TestMinerUParserSupportsOnlyPDF(t *testing.T) {
	p := NewParser(&fakeMinerUClient{}, &fakeRawStore{}, ParserConfig{})
	if !p.Supports("pdf") {
		t.Fatal("Supports(pdf) = false")
	}
	if p.Supports("docx") {
		t.Fatal("Supports(docx) = true, want false")
	}
}

func TestMinerUParserParseReturnsError(t *testing.T) {
	p := NewParser(&fakeMinerUClient{}, &fakeRawStore{}, ParserConfig{})
	_, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "pdf"})
	if err == nil {
		t.Fatal("Parse() should return error for async-only parser")
	}
}

func TestMinerUParserStartReadsPDFAndUploads(t *testing.T) {
	fakeClient := &fakeMinerUClient{}
	fakeStore := &fakeRawStore{content: []byte("%PDF-1.4 fake"), key: "raw/test.pdf"}
	p := NewParser(fakeClient, fakeStore, ParserConfig{ModelVersion: "vlm"})

	start, err := p.Start(context.Background(), parserport.AsyncParseInput{
		FileType:      "pdf",
		RawStorageKey: "raw/test.pdf",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if start.ExternalJobID != "batch-123" {
		t.Fatalf("ExternalJobID = %q", start.ExternalJobID)
	}
	if start.Status != parserport.AsyncSubmitted {
		t.Fatalf("Status = %v", start.Status)
	}
	if fakeClient.uploadURL != "https://upload.example.com/signed" {
		t.Fatalf("upload URL = %q", fakeClient.uploadURL)
	}
}

func TestMinerUParserStartRejectsNonPDF(t *testing.T) {
	p := NewParser(&fakeMinerUClient{}, &fakeRawStore{}, ParserConfig{})
	_, err := p.Start(context.Background(), parserport.AsyncParseInput{
		FileType: "docx",
	})
	if err == nil {
		t.Fatal("expected error for non-PDF")
	}
}

func TestMinerUParserPollRunningReturnsRetryAfter(t *testing.T) {
	fakeClient := &fakeMinerUClient{
		pollResult: &TaskResult{Status: TaskStatusRunning},
	}
	p := NewParser(fakeClient, &fakeRawStore{}, ParserConfig{PollInterval: 15 * time.Second})

	result, err := p.Poll(context.Background(), parserport.AsyncParsePollInput{
		ExternalJobID: "batch-123",
	})
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != parserport.AsyncRunning {
		t.Fatalf("Status = %v, want running", result.Status)
	}
	if result.RetryAfter != 15*time.Second {
		t.Fatalf("RetryAfter = %v", result.RetryAfter)
	}
}

func TestMinerUParserPollSucceededBuildsManifest(t *testing.T) {
	fakeClient := &fakeMinerUClient{
		pollResult: &TaskResult{Status: TaskStatusSucceeded, FullResultURL: "https://result.example.com/full.zip"},
		downloadMD: "# 第一章\n\n这是正文。",
	}
	p := NewParser(fakeClient, &fakeRawStore{}, ParserConfig{})

	result, err := p.Poll(context.Background(), parserport.AsyncParsePollInput{
		ExternalJobID: "batch-123",
	})
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != parserport.AsyncSucceeded {
		t.Fatalf("Status = %v", result.Status)
	}
	if result.Document == nil {
		t.Fatal("Document is nil")
	}
	if !strings.Contains(result.Document.Markdown, "第一章") {
		t.Fatalf("Markdown = %q", result.Document.Markdown)
	}
	if result.Document.Manifest.Parser != "pdf" {
		t.Fatalf("Parser = %q, want pdf", result.Document.Manifest.Parser)
	}
	if len(result.Document.Manifest.Blocks) == 0 {
		t.Fatal("Manifest has no blocks")
	}
}

func TestMinerUParserPollCarriesZipImageCandidates(t *testing.T) {
	// 模拟 MinerU 返回包含 markdown + 图片的 zip
	zipData := buildTestZip(t, map[string][]byte{
		"full.md":          []byte("# 第一章\n\n![图](images/logo.png)"),
		"images/logo.png":  []byte("png-bytes"),
		"images/photo.jpg": []byte("jpg-bytes"),
	})
	fakeClient := &fakeMinerUClient{
		pollResult:  &TaskResult{Status: TaskStatusSucceeded, FullResultURL: "https://result.example.com/full.zip"},
		downloadMD:  "# 第一章\n\n![图](images/logo.png)",
		downloadRaw: zipData,
	}
	p := NewParser(fakeClient, &fakeRawStore{}, ParserConfig{MaxZipImageBytes: 10 * 1024 * 1024})

	result, err := p.Poll(context.Background(), parserport.AsyncParsePollInput{
		ExternalJobID: "batch-123",
	})
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != parserport.AsyncSucceeded {
		t.Fatalf("Status = %v", result.Status)
	}
	if len(result.Document.AssetCandidates) != 2 {
		t.Fatalf("AssetCandidates = %d, want 2", len(result.Document.AssetCandidates))
	}

	byPath := make(map[string]parserport.AssetCandidate, len(result.Document.AssetCandidates))
	for _, c := range result.Document.AssetCandidates {
		byPath[c.RelativePath] = c
	}
	logo, ok := byPath["images/logo.png"]
	if !ok {
		t.Fatal("missing images/logo.png candidate")
	}
	if logo.MimeType != "image/png" || string(logo.Data) != "png-bytes" {
		t.Fatalf("logo candidate = %#v", logo)
	}
	// 图片引用仍保留在 markdown 中，由 AssetResolver 后续替换为 public URL
	if !strings.Contains(result.Document.Markdown, "images/logo.png") {
		t.Fatalf("Markdown = %q, want relative ref preserved", result.Document.Markdown)
	}
}

func TestMinerUParserPollFailedReturnsError(t *testing.T) {
	fakeClient := &fakeMinerUClient{
		pollResult: &TaskResult{Status: TaskStatusFailed, ErrorCode: "parse_error", ErrorMessage: "无法解析"},
	}
	p := NewParser(fakeClient, &fakeRawStore{}, ParserConfig{})

	result, err := p.Poll(context.Background(), parserport.AsyncParsePollInput{
		ExternalJobID: "batch-123",
	})
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != parserport.AsyncFailed {
		t.Fatalf("Status = %v, want failed", result.Status)
	}
	if result.ErrorCode != "parse_error" {
		t.Fatalf("ErrorCode = %q", result.ErrorCode)
	}
}
