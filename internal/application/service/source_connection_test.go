package service

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

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
