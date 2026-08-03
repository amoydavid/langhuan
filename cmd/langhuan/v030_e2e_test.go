//go:build integration

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	hibikenasynq "github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	queueadapter "github.com/dajee/langhuan/internal/adapters/queue/asynq"
	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/interfaces/worker"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	"github.com/dajee/langhuan/internal/testsupport"
)

type v030FakeFactory struct {
	v031FakeFactory
}

func (f v030FakeFactory) NewClient(
	_ context.Context,
	_ embeddingport.ClientInput,
) (embeddingport.EmbeddingClient, error) {
	return v030FakeEmbeddingClient{dimension: f.dimension}, nil
}

type v030FakeEmbeddingClient struct {
	dimension int
}

func (c v030FakeEmbeddingClient) Dimension() int { return c.dimension }

func (c v030FakeEmbeddingClient) Embed(
	_ context.Context,
	input embeddingport.EmbedInput,
) (*embeddingport.EmbedResult, error) {
	vectors := make([][]float32, len(input.Texts))
	for index := range vectors {
		vectors[index] = make([]float32, c.dimension)
	}
	return &embeddingport.EmbedResult{Vectors: vectors}, nil
}

type v030E2E struct {
	t          *testing.T
	ctx        context.Context
	db         *gorm.DB
	redis      *redis.Client
	asynq      *hibikenasynq.Client
	worker     *hibikenasynq.Server
	server     *httptest.Server
	client     *http.Client
	services   *runtimeServices
	rawDir     string
	workspace  *dto.Workspace
	user       *dto.AuthenticatedUser
	providerID uuid.UUID
	modelID    uuid.UUID
}

func TestV030MultiFormatHTTPWorkerE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()

	fixtures := []struct {
		name, mime string
		content    []byte
	}{
		{name: "sample.md", mime: "text/markdown", content: []byte("# 指南\n\nMarkdown 正文")},
		{name: "sample.txt", mime: "text/plain", content: []byte("普通文本\n第二行")},
		{name: "sample.csv", mime: "text/csv", content: []byte("名称,值\n琅嬛,1\n检索,2\n")},
		{name: "sample.xlsx", mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content: v030XLSXFixture(t)},
		{name: "sample.docx", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", content: v030DOCXFixture(t)},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			created := env.upload(fixture.name, fixture.mime, fixture.content, http.StatusCreated)
			document := env.waitReady(created.Document.ID)
			if document.NormalizedMarkdown == "" {
				t.Fatal("normalized markdown is empty")
			}
			env.assertPersistedParseAndChunks(document.ID)
		})
	}
}

func TestV030PDFRejectedWithoutPersistenceE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	before := env.snapshot()
	env.upload("sample.pdf", "application/pdf", []byte("%PDF-1.7"), http.StatusUnsupportedMediaType)
	after := env.snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("PDF side effects: before=%#v after=%#v", before, after)
	}
}

func startV030E2E(t *testing.T) *v030E2E {
	t.Helper()
	ctx := context.Background()
	testDSN := testsupport.NewMigratedPostgres(t)
	gormDB, err := db.Open(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	rawDir := t.TempDir()
	cfg := runtimeServicesConfig(t)
	cfg.Storage.RawDocumentDir = rawDir
	cfg.Ingest.MaxFileSizeBytes = 8 << 20
	cfg.Ingest.AllowedFileTypes = []string{"markdown", "md", "txt", "csv", "xlsx", "docx"}
	cfg.Auth.Session.SecureCookie = false
	cfg.Redis.Addr = testsupport.NewIsolatedRedis(t)
	cfg.Redis.DB = 0

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, DB: cfg.Redis.DB})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	redisOpt := hibikenasynq.RedisClientOpt{Addr: cfg.Redis.Addr, DB: cfg.Redis.DB}
	asynqClient := hibikenasynq.NewClient(redisOpt)
	jobQueue := queueadapter.NewQueue(asynqClient)
	embeddingRegistry, err := embeddingadapter.NewRegistry(v030FakeFactory{
		v031FakeFactory: v031FakeFactory{dimension: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	services, err := buildRuntimeServices(gormDB, cfg, jobQueue, redisClient, embeddingRegistry)
	if err != nil {
		t.Fatal(err)
	}
	mux := hibikenasynq.NewServeMux()
	worker.RegisterDocumentHandlers(mux, worker.DocumentHandlers{
		Store: services.documentTaskStore, Queue: jobQueue, Pipeline: services.pipeline,
	})
	workerServer := hibikenasynq.NewServer(redisOpt, hibikenasynq.Config{Concurrency: 1, Queues: map[string]int{"default": 1}})
	if err := workerServer.Start(mux); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(buildHTTPRouter(services))
	jar, _ := cookiejar.New(nil)
	env := &v030E2E{t: t, ctx: ctx, db: gormDB, redis: redisClient, asynq: asynqClient, worker: workerServer, server: httpServer,
		client: &http.Client{Jar: jar, Timeout: 5 * time.Second}, services: services, rawDir: rawDir}
	t.Cleanup(func() {
		httpServer.Close()
		workerServer.Shutdown()
		_ = asynqClient.Close()
		_ = redisClient.FlushDB(ctx).Err()
		_ = redisClient.Close()
		if env.workspace != nil {
			_ = gormDB.Exec("DELETE FROM workspaces WHERE id = ?", env.workspace.ID).Error
		}
		if env.user != nil {
			_ = gormDB.Exec("DELETE FROM users WHERE id = ?", env.user.ID).Error
		}
		if env.modelID != uuid.Nil {
			_ = gormDB.Exec("DELETE FROM models WHERE id = ?", env.modelID).Error
		}
		if env.providerID != uuid.Nil {
			_ = gormDB.Exec("DELETE FROM model_providers WHERE id = ?", env.providerID).Error
		}
		if sqlDB, dbErr := gormDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return env
}

func (e *v030E2E) registerAndLogin() {
	email := fmt.Sprintf("v030-%d@example.com", time.Now().UnixNano())
	e.user = &dto.AuthenticatedUser{}
	e.jsonRequest(http.MethodPost, "/api/v1/auth/register", map[string]any{"email": email, "nickname": "v030", "password": "Passw0rd!"}, http.StatusCreated, e.user)
	e.jsonRequest(http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": "Passw0rd!"}, http.StatusOK, nil)
}

func (e *v030E2E) createWorkspaceAndKnowledgeBase() {
	slug := fmt.Sprintf("v030-%d", time.Now().UnixNano())
	e.workspace = &dto.Workspace{}
	e.jsonRequest(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "v030 e2e", "slug": slug}, http.StatusCreated, e.workspace)
	e.createPlatformEmbeddingModel()
	kb := &dto.KnowledgeBase{}
	e.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+slug+"/knowledge-bases", map[string]any{
		"name": "fixtures", "embedding_model_id": e.modelID,
		"chunking_config": map[string]int{"chunk_size": 40, "chunk_overlap": 5},
	}, http.StatusCreated, kb)
	e.workspace.Metadata = map[string]any{"kb_id": kb.ID.String()}
}

func (e *v030E2E) createPlatformEmbeddingModel() {
	e.t.Helper()
	if e.modelID != uuid.Nil {
		return
	}
	provider := &dto.ModelProvider{}
	e.jsonRequest(http.MethodPost, "/api/v1/admin/model-providers", map[string]any{
		"name": "v030-openai-" + uuid.NewString(), "display_name": "V030 OpenAI",
		"description": "", "provider": "openai",
		"config":      map[string]any{"timeout_seconds": 60},
		"credentials": map[string]any{"api_key": "e2e-secret"},
	}, http.StatusCreated, provider)
	e.providerID = provider.ID
	configuredModel := &dto.Model{}
	e.jsonRequest(http.MethodPost, "/api/v1/admin/model-providers/"+provider.ID.String()+"/models", map[string]any{
		"name": "v030-embedding-" + uuid.NewString(), "display_name": "V030 Embedding",
		"description": "", "type": "embedding", "model_name": "nomic-embed-text",
		"dimensions": 1024, "parameters": map[string]any{"batch_size": 32},
	}, http.StatusCreated, configuredModel)
	e.modelID = configuredModel.ID
}

func (e *v030E2E) upload(name, mime string, content []byte, wantStatus int) *service.IngestDocumentResult {
	return e.uploadWithClient(e.client, name, mime, content, wantStatus)
}

func (e *v030E2E) uploadWithClient(client *http.Client, name, mime string, content []byte, wantStatus int) *service.IngestDocumentResult {
	e.t.Helper()
	kbID := e.workspace.Metadata["kb_id"].(string)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(map[string][]string)
	partHeader["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, name)}
	partHeader["Content-Type"] = []string{mime}
	part, err := writer.CreatePart(textproto.MIMEHeader(partHeader))
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		e.t.Fatal(err)
	}
	_ = writer.WriteField("source_type", "upload")
	if err := writer.Close(); err != nil {
		e.t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, e.server.URL+"/api/v1/workspaces/"+e.workspace.Slug+"/knowledge-bases/"+kbID+"/documents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		e.t.Fatalf("upload %s status=%d body=%s", name, resp.StatusCode, data)
	}
	if wantStatus != http.StatusCreated {
		return nil
	}
	var result service.IngestDocumentResult
	if err := json.Unmarshal(data, &result); err != nil {
		e.t.Fatal(err)
	}
	return &result
}

func (e *v030E2E) waitReady(id uuid.UUID) *dto.Document {
	e.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var document dto.Document
		e.jsonRequest(http.MethodGet, "/api/v1/workspaces/"+e.workspace.Slug+"/documents/"+id.String(), nil, http.StatusOK, &document)
		if document.Status == value.DocumentStatusReady {
			return &document
		}
		if document.Status == value.DocumentStatusFailed {
			e.t.Fatalf("document failed: %s", document.ErrorMessage)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var documentRow db.DocumentRow
	var revisions []db.DocumentRevisionRow
	var jobs []db.JobRow
	var entries []db.RetrievalEntryRow
	_ = e.db.Where("id = ?", id).First(&documentRow).Error
	_ = e.db.Where("document_id = ?", id).Order("created_at ASC").Find(&revisions).Error
	_ = e.db.Where("document_id = ?", id).Order("created_at ASC").Find(&jobs).Error
	_ = e.db.Where("document_id = ?", id).Order("created_at ASC").Find(&entries).Error
	e.t.Fatalf(
		"document %s did not become ready: status=%q revisions=%#v jobs=%#v entries=%d",
		id, documentRow.Status, revisions, jobs, len(entries),
	)
	return nil
}

func (e *v030E2E) assertPersistedParseAndChunks(id uuid.UUID) {
	e.t.Helper()
	document, err := e.services.documentRepo.Get(e.ctx, e.workspace.ID, id)
	if err != nil {
		e.t.Fatal(err)
	}
	if document.ActiveRevision == nil || document.ActiveRevision.ProcessingVersion != 1 ||
		document.ActiveRevision.ParseManifest == nil || document.ActiveRevision.ParseManifest.Version != 1 ||
		len(document.ActiveRevision.ParseManifest.Blocks) == 0 {
		e.t.Fatalf("active revision parse persistence = %#v", document.ActiveRevision)
	}
	var count int64
	err = e.db.WithContext(e.ctx).Model(&db.ChunkRow{}).
		Where("workspace_id = ? AND document_id = ?", e.workspace.ID, id).
		Count(&count).Error
	if err != nil || count == 0 {
		e.t.Fatalf("chunk count=%d err=%v", count, err)
	}
}

func (e *v030E2E) jsonRequest(method, path string, body any, want int, output any) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, e.server.URL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		e.t.Fatalf("%s %s status=%d body=%s", method, path, resp.StatusCode, data)
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			e.t.Fatal(err)
		}
	}
}

type v030Snapshot struct {
	documents int64
	jobs      int64
	rawFiles  map[string][sha256.Size]byte
}

func (e *v030E2E) snapshot() v030Snapshot {
	result := v030Snapshot{rawFiles: make(map[string][sha256.Size]byte)}
	if err := e.db.Model(&db.DocumentRow{}).Count(&result.documents).Error; err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.Model(&db.JobRow{}).Count(&result.jobs).Error; err != nil {
		e.t.Fatal(err)
	}
	if err := filepath.WalkDir(e.rawDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			relative, relErr := filepath.Rel(e.rawDir, path)
			if relErr != nil {
				return relErr
			}
			result.rawFiles[relative] = sha256.Sum256(content)
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return result
}

func v030XLSXFixture(t *testing.T) []byte {
	book := excelize.NewFile()
	defer book.Close()
	_ = book.SetSheetRow("Sheet1", "A1", &[]any{"名称", "值"})
	_ = book.SetSheetRow("Sheet1", "A2", &[]any{"琅嬛", 1})
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func v030DOCXFixture(t *testing.T) []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>指南</w:t></w:r></w:p><w:p><w:r><w:t>DOCX 正文</w:t></w:r></w:p></w:body></w:document>`))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
