package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

type fakeSourceConnectionRepo struct {
	mu    sync.Mutex
	data  map[uuid.UUID]*model.SourceConnection
	errOn string
}

func newFakeSourceConnectionRepo() *fakeSourceConnectionRepo {
	return &fakeSourceConnectionRepo{data: map[uuid.UUID]*model.SourceConnection{}}
}

func (r *fakeSourceConnectionRepo) Create(ctx context.Context, conn *model.SourceConnection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[conn.ID] = conn
	return nil
}

func (r *fakeSourceConnectionRepo) Get(ctx context.Context, workspaceID, id uuid.UUID) (*model.SourceConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn, ok := r.data[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return conn, nil
}

func (r *fakeSourceConnectionRepo) List(ctx context.Context, workspaceID uuid.UUID) ([]*model.SourceConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*model.SourceConnection, 0, len(r.data))
	for _, conn := range r.data {
		result = append(result, conn)
	}
	return result, nil
}

func (r *fakeSourceConnectionRepo) Update(ctx context.Context, conn *model.SourceConnection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[conn.ID] = conn
	return nil
}

func (r *fakeSourceConnectionRepo) SoftDelete(ctx context.Context, workspaceID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, id)
	return nil
}

// fakeCipher 记录调用，返回可辨识的密文前缀。
type fakeCipher struct {
	mu       sync.Mutex
	encrypts map[uuid.UUID]int
}

func newFakeCipher() *fakeCipher {
	return &fakeCipher{encrypts: map[uuid.UUID]int{}}
}

func (c *fakeCipher) Encrypt(id uuid.UUID, plaintext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.encrypts[id]++
	return append([]byte("enc:"), plaintext...), nil
}

func (c *fakeCipher) Decrypt(id uuid.UUID, ciphertext []byte) ([]byte, error) {
	return bytes.TrimPrefix(ciphertext, []byte("enc:")), nil
}

func (c *fakeCipher) encryptCount(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encrypts[id]
}

func newSourceConnectionServiceForTest(t *testing.T) (*SourceConnectionService, *fakeSourceConnectionRepo, *fakeCipher) {
	t.Helper()
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	svc := NewSourceConnectionService(SourceConnectionServiceDeps{Repository: repo, Cipher: cipher})
	return svc, repo, cipher
}

func TestCreateSourceConnectionEncryptsSecret(t *testing.T) {
	svc, repo, cipher := newSourceConnectionServiceForTest(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, CreateSourceConnectionInput{
		WorkspaceID: uuid.New(), Provider: "feishu", Name: "主公司飞书", AppID: "cli_a1", AppSecret: "secret-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AppID != "cli_a1" {
		t.Fatalf("app_id = %q", created.AppID)
	}
	// DTO 不应携带 secret
	if created.Status != "active" {
		t.Fatalf("status = %q", created.Status)
	}
	// 落库的应是密文，不是明文
	stored := repo.data[created.ID]
	if !bytes.Equal(stored.CredentialsCiphertext, []byte("enc:secret-a")) {
		t.Fatalf("ciphertext = %q, want enc:secret-a", stored.CredentialsCiphertext)
	}
	if cipher.encryptCount(created.ID) != 1 {
		t.Fatal("secret should be encrypted exactly once")
	}
}

func TestListSourceConnectionsHidesSecret(t *testing.T) {
	svc, _, _ := newSourceConnectionServiceForTest(t)
	ctx := context.Background()
	ws := uuid.New()
	_, _ = svc.Create(ctx, CreateSourceConnectionInput{WorkspaceID: ws, Provider: "feishu", Name: "a", AppID: "cli", AppSecret: "s"})
	listed, err := svc.List(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("len = %d", len(listed))
	}
	// SourceConnection DTO 结构体不含 secret 字段，类型层面已保证；这里断言 app_id 可见
	if listed[0].AppID != "cli" {
		t.Fatal("app_id missing")
	}
}

func TestUpdateRotatesSecret(t *testing.T) {
	svc, repo, cipher := newSourceConnectionServiceForTest(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, CreateSourceConnectionInput{WorkspaceID: uuid.New(), Provider: "feishu", Name: "a", AppID: "cli", AppSecret: "old"})
	newSecret := "new-secret"
	if _, err := svc.Update(ctx, UpdateSourceConnectionInput{WorkspaceID: created.WorkspaceID, ID: created.ID, AppSecret: &newSecret}); err != nil {
		t.Fatal(err)
	}
	stored := repo.data[created.ID]
	if !bytes.Equal(stored.CredentialsCiphertext, []byte("enc:new-secret")) {
		t.Fatalf("ciphertext = %q after rotation", stored.CredentialsCiphertext)
	}
	if cipher.encryptCount(created.ID) != 2 {
		t.Fatal("rotation should encrypt a second time")
	}
}

func TestSelectorDecryptsSecretForRunner(t *testing.T) {
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	svc := NewSourceConnectionService(SourceConnectionServiceDeps{Repository: repo, Cipher: cipher})
	selector := NewSourceConnectionSelector(repo, cipher)
	ctx := context.Background()
	created, _ := svc.Create(ctx, CreateSourceConnectionInput{WorkspaceID: uuid.New(), Provider: "feishu", Name: "a", AppID: "cli", AppSecret: "runner-secret"})

	selected, err := selector.Select(ctx, created.WorkspaceID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(selected.AppSecret) != "runner-secret" {
		t.Fatalf("app_secret = %q, want runner-secret", selected.AppSecret)
	}
	if selected.AppID != "cli" {
		t.Fatalf("app_id = %q", selected.AppID)
	}
}

func TestCreateRejectsEmptySecret(t *testing.T) {
	svc, _, _ := newSourceConnectionServiceForTest(t)
	_, err := svc.Create(context.Background(), CreateSourceConnectionInput{
		WorkspaceID: uuid.New(), Provider: "feishu", Name: "a", AppID: "cli", AppSecret: "",
	})
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

// TestCreateProviderCredentialMatrix 固定 T12 的 service 层凭证分支：
// feishu/firecrawl/jina 必须凭证；browser 允许空凭证（token 可选），且空凭证
// 落库为空密文、不触发加密（selector 对空密文按空凭证处理）。
func TestCreateProviderCredentialMatrix(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		appID         string
		appSecret     string
		config        map[string]any
		wantErr       bool
		errContains   string
		wantEncrypted bool
	}{
		{name: "feishu 必须凭证", provider: "feishu", appID: "cli", appSecret: "s", wantEncrypted: true},
		{name: "firecrawl 凭证即 api_key", provider: "firecrawl", appSecret: "fc-key", wantEncrypted: true},
		{name: "firecrawl 缺凭证", provider: "firecrawl", wantErr: true, errContains: "app_secret"},
		{name: "jina 凭证即 api_key", provider: "jina", appSecret: "jina-key", wantEncrypted: true},
		{name: "jina 缺凭证", provider: "jina", wantErr: true, errContains: "app_secret"},
		{name: "browser 无凭证（CDP endpoint 在 config）", provider: "browser", config: map[string]any{"endpoint_url": "ws://cdp:9222"}},
		{name: "browser 可选 token", provider: "browser", appSecret: "tok", config: map[string]any{"endpoint_url": "ws://cdp:9222"}, wantEncrypted: true},
		{name: "browser 缺 endpoint", provider: "browser", wantErr: true, errContains: "endpoint_url"},
		{name: "未知 provider 空凭证仍拒绝", provider: "notion", wantErr: true, errContains: "provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, cipher := newSourceConnectionServiceForTest(t)
			created, err := svc.Create(context.Background(), CreateSourceConnectionInput{
				WorkspaceID: uuid.New(), Provider: tt.provider, Name: "连接",
				AppID: tt.appID, AppSecret: tt.appSecret, Config: tt.config,
			})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("err = %v, want 含 %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored := repo.data[created.ID]
			if tt.wantEncrypted {
				want := append([]byte("enc:"), tt.appSecret...)
				if !bytes.Equal(stored.CredentialsCiphertext, want) {
					t.Fatalf("ciphertext = %q, want %q", stored.CredentialsCiphertext, want)
				}
				if cipher.encryptCount(created.ID) != 1 {
					t.Fatal("secret should be encrypted exactly once")
				}
				return
			}
			// browser 无凭证：不加密、落库空密文。
			if len(stored.CredentialsCiphertext) != 0 {
				t.Fatalf("ciphertext = %v, want empty", stored.CredentialsCiphertext)
			}
			if cipher.encryptCount(created.ID) != 0 {
				t.Fatal("empty secret must not be encrypted")
			}
			if got := stored.Config["endpoint_url"]; got != "ws://cdp:9222" {
				t.Fatalf("config.endpoint_url = %v, want ws://cdp:9222", got)
			}
		})
	}
}

// TestCreateRejectsAppIDForNonFeishu 断言 service 层透传领域校验：非飞书 provider
// 提交 app_id 显式报错（HTTP 层已先行拦截，这里是纵深防御）。
func TestCreateRejectsAppIDForNonFeishu(t *testing.T) {
	svc, _, _ := newSourceConnectionServiceForTest(t)
	_, err := svc.Create(context.Background(), CreateSourceConnectionInput{
		WorkspaceID: uuid.New(), Provider: "firecrawl", Name: "x", AppID: "cli_a1", AppSecret: "k",
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestSelectorToleratesEmptyCiphertext 覆盖 browser 无 token 连接的解密路径：
// 空密文不调用 cipher.Decrypt，直接返回空凭证（否则 AES-GCM 对空输入必然报错，
// 同步编排与连接测试都会被「密文格式无效」卡死）。
func TestSelectorToleratesEmptyCiphertext(t *testing.T) {
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	svc := NewSourceConnectionService(SourceConnectionServiceDeps{Repository: repo, Cipher: cipher})
	selector := NewSourceConnectionSelector(repo, cipher)
	created, err := svc.Create(context.Background(), CreateSourceConnectionInput{
		WorkspaceID: uuid.New(), Provider: "browser", Name: "cdp",
		Config: map[string]any{"endpoint_url": "ws://cdp:9222"},
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selector.Select(context.Background(), created.WorkspaceID, created.ID)
	if err != nil {
		t.Fatalf("Select on empty ciphertext: %v", err)
	}
	if len(selected.AppSecret) != 0 {
		t.Fatalf("app_secret = %q, want empty", selected.AppSecret)
	}
	if selected.Provider != "browser" {
		t.Fatalf("provider = %q", selected.Provider)
	}
	if got := selected.Config["endpoint_url"]; got != "ws://cdp:9222" {
		t.Fatalf("config.endpoint_url = %v", got)
	}
}

// 确保 now 可注入（Update 时间戳使用注入值）。
func TestServiceNowInjectable(t *testing.T) {
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := &SourceConnectionService{repo: repo, cipher: cipher, now: func() time.Time { return fixed }}
	created, _ := svc.Create(context.Background(), CreateSourceConnectionInput{WorkspaceID: uuid.New(), Provider: "feishu", Name: "a", AppID: "cli", AppSecret: "s"})
	name := "renamed"
	if _, err := svc.Update(context.Background(), UpdateSourceConnectionInput{WorkspaceID: created.WorkspaceID, ID: created.ID, Name: &name}); err != nil {
		t.Fatal(err)
	}
	if !repo.data[created.ID].UpdatedAt.Equal(fixed) {
		t.Fatalf("updated_at = %v, want %v", repo.data[created.ID].UpdatedAt, fixed)
	}
}
