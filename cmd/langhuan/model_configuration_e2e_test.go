//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	ollamaembedding "github.com/dajee/langhuan/internal/adapters/embedding/ollama"
	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	"github.com/dajee/langhuan/internal/testsupport"
)

type v031FakeFactory struct {
	dimension int
}

func (f v031FakeFactory) Provider() string { return "openai" }

func (f v031FakeFactory) CredentialFields() []string { return []string{"api_key"} }

func (f v031FakeFactory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	var config struct {
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if err := decodeV031Strict(input.Config, &config); err != nil || config.TimeoutSeconds < 1 {
		return nil, nil, domainerrors.ErrInvalidProviderConfig
	}
	var credentials struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeV031Strict(input.Credentials, &credentials); err != nil {
		return nil, nil, domainerrors.ErrInvalidProviderConfig
	}
	credentials.APIKey = strings.TrimSpace(credentials.APIKey)
	if credentials.APIKey == "" {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	credentialsJSON, err := json.Marshal(credentials)
	return map[string]any{"timeout_seconds": config.TimeoutSeconds}, credentialsJSON, err
}

func (f v031FakeFactory) DecodeModel(input embeddingport.ModelDecodeInput) (map[string]any, error) {
	var parameters struct {
		BatchSize int `json:"batch_size"`
	}
	if err := decodeV031Strict(input.Parameters, &parameters); err != nil || parameters.BatchSize < 1 {
		return nil, domainerrors.ErrInvalidProviderConfig
	}
	if strings.TrimSpace(input.ModelName) == "" {
		return nil, domainerrors.ErrInvalidProviderConfig
	}
	if input.Dimensions != f.dimension {
		return nil, domainerrors.ErrUnsupportedEmbeddingDimension
	}
	return map[string]any{"batch_size": parameters.BatchSize}, nil
}

func (f v031FakeFactory) NewClient(_ context.Context, input embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	if !bytes.Contains(input.CredentialsJSON, []byte("e2e-secret")) {
		return nil, domainerrors.ErrAuthenticationFailed
	}
	return v031FakeEmbeddingClient{dimension: f.dimension}, nil
}

type v031FakeEmbeddingClient struct {
	dimension int
}

func (c v031FakeEmbeddingClient) Dimension() int { return c.dimension }

func (c v031FakeEmbeddingClient) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	if len(input.Texts) != 1 || input.Texts[0] != "langhuan embedding connection test" {
		return nil, domainerrors.ErrConnectionTestFailed
	}
	return &embeddingport.EmbedResult{Vectors: [][]float32{make([]float32, c.dimension)}}, nil
}

func decodeV031Strict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

type v031E2E struct {
	t      *testing.T
	ctx    context.Context
	db     *gorm.DB
	redis  *redis.Client
	server *httptest.Server
	owner  *http.Client
}

func TestV031ModelConfigurationSelectionE2E(t *testing.T) {
	env := startV031ModelConfigurationE2E(t)
	ownerID := env.registerOwner("v031-owner")
	workspaceA := env.createWorkspace(env.owner, "V031 A")
	workspaceB := env.createWorkspace(env.owner, "V031 B")
	memberA := env.inviteUser(env.owner, workspaceA, value.RoleMember)
	adminA := env.inviteUser(env.owner, workspaceA, value.RoleAdmin)

	shared := env.createProviderAndModel(env.owner, "/api/v1/admin", "shared", 1024)
	env.assertStatus(memberA, http.MethodGet, "/api/v1/workspaces/"+workspaceA.Slug+"/models/"+shared.ModelID.String(), nil, http.StatusOK, nil)
	knowledgeBase := &dto.KnowledgeBase{}
	env.assertStatus(memberA, http.MethodPost, "/api/v1/workspaces/"+workspaceA.Slug+"/knowledge-bases", map[string]any{
		"name": "shared-model-kb", "embedding_model_id": shared.ModelID,
	}, http.StatusCreated, knowledgeBase)
	env.assertErrorCode(env.owner, http.MethodDelete, "/api/v1/admin/models/"+shared.ModelID.String(), nil, http.StatusConflict, "model_in_use")

	own := env.createProviderAndModel(adminA, "/api/v1/workspaces/"+workspaceA.Slug, "workspace-own", 1024)
	env.assertStatus(adminA, http.MethodGet, "/api/v1/workspaces/"+workspaceA.Slug+"/models/"+own.ModelID.String(), nil, http.StatusOK, nil)
	env.assertStatus(env.owner, http.MethodGet, "/api/v1/workspaces/"+workspaceB.Slug+"/models/"+own.ModelID.String(), nil, http.StatusNotFound, nil)
	env.assertStatus(memberA, http.MethodPost, "/api/v1/workspaces/"+workspaceA.Slug+"/model-providers", validV031ProviderBody("member-forbidden"), http.StatusForbidden, nil)
	connection := &dto.ConnectionTestResult{}
	env.assertStatus(adminA, http.MethodPost, "/api/v1/workspaces/"+workspaceA.Slug+"/models/"+own.ModelID.String()+"/test", nil, http.StatusOK, connection)
	if !connection.OK || connection.Dimensions == nil || *connection.Dimensions != 1024 {
		t.Fatalf("connection result = %#v", connection)
	}

	env.assertErrorCode(env.owner, http.MethodPost, "/api/v1/admin/model-providers", map[string]any{
		"name": "qianfan", "display_name": "Qianfan", "provider": "qianfan",
		"config": map[string]any{}, "credentials": map[string]any{},
	}, http.StatusBadRequest, "unsupported_provider")
	env.assertErrorCode(adminA, http.MethodPost, "/api/v1/workspaces/"+workspaceA.Slug+"/model-providers", map[string]any{
		"name": "workspace-ollama", "display_name": "Workspace Ollama", "provider": "ollama",
		"config":      map[string]any{"base_url": "http://127.0.0.1:11434", "timeout_seconds": 60},
		"credentials": map[string]any{},
	}, http.StatusBadRequest, "provider_scope_not_allowed")

	var storedModelIDText string
	if err := env.db.Raw(`SELECT generation.embedding_model_id::text
		FROM knowledge_bases AS knowledge_base
		JOIN knowledge_base_index_generations AS generation
		  ON generation.workspace_id = knowledge_base.workspace_id
		 AND generation.knowledge_base_id = knowledge_base.id
		 AND generation.id = knowledge_base.active_index_generation_id
		WHERE knowledge_base.id = ?`, knowledgeBase.ID).Scan(&storedModelIDText).Error; err != nil {
		t.Fatal(err)
	}
	storedModelID, err := uuid.Parse(storedModelIDText)
	if err != nil {
		t.Fatal(err)
	}
	if storedModelID != shared.ModelID {
		t.Fatalf("stored model = %s, want %s", storedModelID, shared.ModelID)
	}
	var providerRow db.ModelProviderRow
	if err := env.db.Where("id = ?", own.ProviderID).First(&providerRow).Error; err != nil {
		t.Fatal(err)
	}
	ciphertext := providerRow.CredentialsCiphertext
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("e2e-secret")) {
		t.Fatalf("credentials are not encrypted: %q", ciphertext)
	}
	var embeddingCount int64
	if err := env.db.Table("retrieval_entries").Count(&embeddingCount).Error; err != nil {
		t.Fatal(err)
	}
	if embeddingCount != 0 {
		t.Fatalf("retrieval_entries count = %d, want 0", embeddingCount)
	}
	if ownerID == uuid.Nil {
		t.Fatal("owner id is nil")
	}
}

type v031ConfiguredModel struct {
	ProviderID uuid.UUID
	ModelID    uuid.UUID
}

func startV031ModelConfigurationE2E(t *testing.T) *v031E2E {
	t.Helper()
	dsn := testsupport.NewMigratedPostgres(t)
	ctx := context.Background()
	gormDB, _, err := db.Open(config.DatabaseConfig{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	redisAddr := testsupport.NewIsolatedRedis(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeServicesConfig(t)
	cfg.Auth.Session.SecureCookie = false
	cfg.Redis.Addr = redisAddr
	cfg.Redis.DB = 0
	registry, err := embeddingadapter.NewRegistry(v031FakeFactory{dimension: 1024}, ollamaembedding.NewFactory())
	if err != nil {
		t.Fatal(err)
	}
	parserRegistry, err := buildRuntimeParserProviderRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rerankRegistry, err := buildRuntimeRerankRegistry()
	if err != nil {
		t.Fatal(err)
	}
	services, err := buildRuntimeServices(ctx, gormDB, cfg, nil, redisClient, registry, rerankRegistry, parserRegistry, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(buildHTTPRouter(services))
	owner := newCookieClient(t)
	env := &v031E2E{t: t, ctx: ctx, db: gormDB, redis: redisClient, server: server, owner: owner}
	t.Cleanup(func() {
		server.Close()
		_ = redisClient.FlushDB(ctx).Err()
		_ = redisClient.Close()
		sqlDB, dbErr := gormDB.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return env
}

func (e *v031E2E) registerOwner(prefix string) uuid.UUID {
	e.t.Helper()
	email := prefix + "-" + uuid.NewString() + "@example.com"
	var user dto.AuthenticatedUser
	e.assertStatus(e.owner, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": email, "nickname": "V031 Owner", "password": "Passw0rd!",
	}, http.StatusCreated, &user)
	e.assertStatus(e.owner, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": email, "password": "Passw0rd!",
	}, http.StatusOK, nil)
	return user.ID
}

func (e *v031E2E) createWorkspace(client *http.Client, name string) *dto.Workspace {
	e.t.Helper()
	workspace := &dto.Workspace{}
	e.assertStatus(client, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": name, "slug": "v031-" + uuid.NewString(),
	}, http.StatusCreated, workspace)
	return workspace
}

func (e *v031E2E) inviteUser(owner *http.Client, workspace *dto.Workspace, role value.WorkspaceRole) *http.Client {
	e.t.Helper()
	email := "v031-" + string(role) + "-" + uuid.NewString() + "@example.com"
	var invitation struct {
		InviteURL string `json:"invite_url"`
	}
	e.assertStatus(owner, http.MethodPost, "/api/v1/workspaces/"+workspace.Slug+"/invitations", map[string]any{
		"invited_email": email, "role": role,
	}, http.StatusCreated, &invitation)
	parsed, err := url.Parse(invitation.InviteURL)
	if err != nil {
		e.t.Fatal(err)
	}
	token := path.Base(parsed.Path)
	client := newCookieClient(e.t)
	e.assertStatus(client, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": email, "nickname": "V031 " + string(role), "password": "Passw0rd!", "invitation_token": token,
	}, http.StatusCreated, nil)
	return client
}

func (e *v031E2E) createProviderAndModel(client *http.Client, basePath, name string, dimensions int) v031ConfiguredModel {
	e.t.Helper()
	provider := &dto.ModelProvider{}
	e.assertStatus(client, http.MethodPost, basePath+"/model-providers", validV031ProviderBody(name), http.StatusCreated, provider)
	configuredModel := &dto.Model{}
	e.assertStatus(client, http.MethodPost, basePath+"/model-providers/"+provider.ID.String()+"/models", map[string]any{
		"name": name + "-embedding", "display_name": name + " Embedding", "description": "",
		"type": "embedding", "model_name": "text-embedding-test", "dimensions": dimensions,
		"parameters": map[string]any{"batch_size": 32},
	}, http.StatusCreated, configuredModel)
	return v031ConfiguredModel{ProviderID: provider.ID, ModelID: configuredModel.ID}
}

func validV031ProviderBody(name string) map[string]any {
	return map[string]any{
		"name": name + "-provider", "display_name": name + " Provider", "description": "",
		"provider": "openai", "config": map[string]any{"timeout_seconds": 60},
		"credentials": map[string]any{"api_key": "e2e-secret"},
	}
}

func (e *v031E2E) assertStatus(client *http.Client, method, requestPath string, body any, status int, output any) *http.Response {
	e.t.Helper()
	return doJSON(e.t, client, e.server.URL, method, requestPath, body, status, output)
}

func (e *v031E2E) assertErrorCode(client *http.Client, method, requestPath string, body any, status int, code string) {
	e.t.Helper()
	response := e.assertStatus(client, method, requestPath, body, status, nil)
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeResponse(e.t, response, &result)
	if result.Error.Code != code {
		e.t.Fatalf("error code = %q, want %q", result.Error.Code, code)
	}
}

var _ embeddingport.Factory = v031FakeFactory{}
var _ embeddingport.EmbeddingClient = v031FakeEmbeddingClient{}
