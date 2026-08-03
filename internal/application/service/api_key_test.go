package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// fakeAPIKeyStore 是 WorkspaceAPIKeyStore 的内存测试替身。
type fakeAPIKeyStore struct {
	keys           map[uuid.UUID]*model.WorkspaceAPIKey
	ciphertexts    map[uuid.UUID][]byte
	activeCount    int
	createErr      error
	updateErr      error
	touchCalls     int
	touchErr       error
	revokeErr      error
	countActiveErr error
}

func newFakeAPIKeyStore() *fakeAPIKeyStore {
	return &fakeAPIKeyStore{
		keys:        make(map[uuid.UUID]*model.WorkspaceAPIKey),
		ciphertexts: make(map[uuid.UUID][]byte),
	}
}

func (s *fakeAPIKeyStore) WithinWorkspace(ctx context.Context, workspaceID uuid.UUID, fn func(context.Context, WorkspaceAPIKeyTx) error) error {
	return fn(ctx, &fakeAPIKeyTx{store: s})
}
func (s *fakeAPIKeyStore) FindByTokenHashWithBindings(ctx context.Context, tokenHash string) (*model.WorkspaceAPIKey, error) {
	for _, k := range s.keys {
		if k.TokenHash == tokenHash {
			return k, nil
		}
	}
	return nil, domainerrors.ErrNotFound
}
func (s *fakeAPIKeyStore) RevealSecretCiphertext(ctx context.Context, workspaceID, keyID uuid.UUID) ([]byte, error) {
	ct, ok := s.ciphertexts[keyID]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return ct, nil
}
func (s *fakeAPIKeyStore) Get(ctx context.Context, workspaceID, keyID uuid.UUID) (*model.WorkspaceAPIKey, error) {
	k, ok := s.keys[keyID]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return k, nil
}
func (s *fakeAPIKeyStore) List(ctx context.Context, workspaceID uuid.UUID) ([]*model.WorkspaceAPIKey, error) {
	out := make([]*model.WorkspaceAPIKey, 0, len(s.keys))
	for _, k := range s.keys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (s *fakeAPIKeyStore) Revoke(ctx context.Context, workspaceID, keyID, actorID uuid.UUID, now time.Time) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}
	k, ok := s.keys[keyID]
	if !ok {
		return domainerrors.ErrNotFound
	}
	k.RevokedAt = &now
	if actorID != uuid.Nil {
		k.RevokedBy = &actorID
	}
	return nil
}
func (s *fakeAPIKeyStore) CountActive(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error) {
	if s.countActiveErr != nil {
		return 0, s.countActiveErr
	}
	return s.activeCount, nil
}
func (s *fakeAPIKeyStore) TouchLastUsed(ctx context.Context, workspaceID, keyID uuid.UUID, now time.Time, minInterval time.Duration) error {
	s.touchCalls++
	return s.touchErr
}

type fakeAPIKeyTx struct{ store *fakeAPIKeyStore }

func (tx *fakeAPIKeyTx) CreateWithKnowledgeBaseBindings(ctx context.Context, record WorkspaceAPIKeyCreateRecord) error {
	if tx.store.createErr != nil {
		return tx.store.createErr
	}
	// 绑定集合在领域模型上回填，便于测试查询路径复现真实读路径。
	record.Key.KnowledgeBaseIDs = append([]uuid.UUID(nil), record.KnowledgeBaseIDs...)
	tx.store.keys[record.Key.ID] = record.Key
	tx.store.ciphertexts[record.Key.ID] = record.SecretCiphertext
	return nil
}

func (tx *fakeAPIKeyTx) UpdateKnowledgeBaseScope(
	ctx context.Context,
	workspaceID, keyID uuid.UUID,
	knowledgeBaseIDs []uuid.UUID,
	scopes []value.APIScope,
	name string,
	expiresAt *time.Time,
	now time.Time,
) error {
	if tx.store.updateErr != nil {
		return tx.store.updateErr
	}
	k, ok := tx.store.keys[keyID]
	if !ok || k.RevokedAt != nil {
		// 与真实 store 一致：不存在或已吊销视为不可修改终态。
		return domainerrors.ErrAPIKeyImmutable
	}
	k.Name = name
	k.Scopes = append([]value.APIScope(nil), scopes...)
	k.ExpiresAt = expiresAt
	k.UpdatedAt = now
	k.KnowledgeBaseIDs = append([]uuid.UUID(nil), knowledgeBaseIDs...)
	return nil
}

// fakeAPIKeyNameStore 返回固定可读名称。
type fakeAPIKeyNameStore struct {
	kbNames map[uuid.UUID]string
	users   map[uuid.UUID]string
}

func (n *fakeAPIKeyNameStore) KnowledgeBaseNames(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		// 与真实 APIKeyNameStore 行为一致：仅返回存在的 KB，缺失的不进 map。
		if name, ok := n.kbNames[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}
func (n *fakeAPIKeyNameStore) UserNickname(ctx context.Context, userID uuid.UUID) (string, error) {
	return n.users[userID], nil
}

type apiKeyFixture struct {
	svc       *APIKeyService
	store     *fakeAPIKeyStore
	names     *fakeAPIKeyNameStore
	cipher    *recordingCipher
	now       time.Time
	workspace uuid.UUID
	adminID   uuid.UUID
	kbIDs     []uuid.UUID
}

func newAPIKeyFixture(t *testing.T) *apiKeyFixture {
	t.Helper()
	urls, err := NewPublicURLBuilder("https://langhuan.example.com")
	require.NoError(t, err)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	store := newFakeAPIKeyStore()
	names := &fakeAPIKeyNameStore{
		kbNames: map[uuid.UUID]string{},
		users:   map[uuid.UUID]string{},
	}
	cipher := &recordingCipher{}
	workspace := uuid.New()
	adminID := uuid.New()
	kbIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for _, id := range kbIDs {
		names.kbNames[id] = "KB-" + id.String()[:4]
	}
	names.users[adminID] = "管理员"
	svc, err := NewAPIKeyService(APIKeyServiceDeps{
		Store:  store,
		Cipher: cipher,
		Names:  names,
		URLs:   urls,
		Config: config.APIKeyConfig{
			DefaultLifetimeSeconds:       7776000,
			MaxLifetimeSeconds:           31536000,
			LastUsedTouchIntervalSeconds: 300,
			ActiveLimit:                  100,
		},
		Random: rand.Reader,
		Now:    func() time.Time { return now },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	return &apiKeyFixture{svc: svc, store: store, names: names, cipher: cipher, now: now, workspace: workspace, adminID: adminID, kbIDs: kbIDs}
}

// recordingCipher 包装真实 AES-GCM cipher，便于测试观察加解密。
type recordingCipher struct {
	encryptCalls int
	decryptCalls int
	failDecrypt  bool
}

func (c *recordingCipher) Encrypt(workspaceID, apiKeyID uuid.UUID, plaintext []byte) ([]byte, error) {
	c.encryptCalls++
	// 用简单 envelope：版本字节 + 明文，便于测试 reveal。
	return append([]byte{0x01}, plaintext...), nil
}
func (c *recordingCipher) Decrypt(workspaceID, apiKeyID uuid.UUID, ciphertext []byte) ([]byte, error) {
	c.decryptCalls++
	if c.failDecrypt {
		return nil, domainerrors.ErrAPIKeySecretUnavailable
	}
	if len(ciphertext) < 1 || ciphertext[0] != 0x01 {
		return nil, domainerrors.ErrAPIKeySecretUnavailable
	}
	return ciphertext[1:], nil
}

func TestAPIKeyServiceCreateNeverAndRevealAfterRevoke(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "检索 Agent", KnowledgeBaseIDs: f.kbIDs,
		Scopes:     []value.APIScope{value.ScopeDocumentsRead, value.ScopeSearchRead},
		Expiration: APIKeyExpiration{Type: ExpirationNever},
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.APIKey)
	require.Nil(t, created.Item.ExpiresAt)
	require.Len(t, created.Item.KnowledgeBases, 2)

	// 吊销后仍可 reveal。
	require.NoError(t, f.svc.Revoke(context.Background(), f.workspace, f.adminID, value.RoleAdmin, created.Item.ID))
	revealed, err := f.svc.Reveal(context.Background(), f.workspace, value.RoleAdmin, created.Item.ID)
	require.NoError(t, err)
	require.Equal(t, created.APIKey, revealed.APIKey)
}

func TestAPIKeyServiceCreateDefaultsTo90Days(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "默认到期", KnowledgeBaseIDs: f.kbIDs,
		Scopes:     []value.APIScope{value.ScopeSearchRead},
		Expiration: APIKeyExpiration{},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Item.ExpiresAt)
	want := f.now.Add(90 * 24 * time.Hour)
	require.WithinDuration(t, want, *created.Item.ExpiresAt, time.Second)
}

func TestAPIKeyServiceCreateRejectsMemberRole(t *testing.T) {
	f := newAPIKeyFixture(t)
	_, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: uuid.New(), ActorRole: value.RoleMember,
		Name: "member", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrForbidden)
}

func TestAPIKeyServiceCreateRejectsInvalidScopeAndEmptyKB(t *testing.T) {
	f := newAPIKeyFixture(t)
	_, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "bad scope", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{"admin"},
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)

	_, err = f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "no kb", KnowledgeBaseIDs: nil, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)
}

func TestAPIKeyServiceCreateEnforcesActiveLimit(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.store.activeCount = 100
	_, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "limit", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrAPIKeyLimitReached)
}

func TestAPIKeyServiceUpdateAllFields(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "旧名称", KnowledgeBaseIDs: f.kbIDs,
		Scopes:     []value.APIScope{value.ScopeSearchRead},
		Expiration: APIKeyExpiration{Type: ExpirationNever},
	})
	require.NoError(t, err)

	// 缩减到单个 KB、改名、加 scope、设到期。
	newKB := f.kbIDs[0]
	updated, err := f.svc.Update(context.Background(), UpdateAPIKeyInput{
		WorkspaceID: f.workspace, KeyID: created.Item.ID, ActorRole: value.RoleAdmin,
		Name: "新名称", KnowledgeBaseIDs: []uuid.UUID{newKB},
		Scopes:     []value.APIScope{value.ScopeDocumentsRead, value.ScopeSearchRead},
		Expiration: APIKeyExpiration{Type: ExpirationDays, Days: 30},
	})
	require.NoError(t, err)
	require.Equal(t, "新名称", updated.Name)
	require.Len(t, updated.KnowledgeBases, 1)
	require.Equal(t, newKB, updated.KnowledgeBases[0].ID)
	require.ElementsMatch(t, []value.APIScope{value.ScopeDocumentsRead, value.ScopeSearchRead}, updated.Scopes)
	require.NotNil(t, updated.ExpiresAt)
	want := f.now.Add(30 * 24 * time.Hour)
	require.WithinDuration(t, want, *updated.ExpiresAt, time.Second)
}

func TestAPIKeyServiceUpdateRejectsMemberRole(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "member-update", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	_, err = f.svc.Update(context.Background(), UpdateAPIKeyInput{
		WorkspaceID: f.workspace, KeyID: created.Item.ID, ActorRole: value.RoleMember,
		Name: "x", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrForbidden)
}

func TestAPIKeyServiceUpdateRejectsInvalidScopeAndEmptyKB(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "bad", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)

	// 非法 scope。
	_, err = f.svc.Update(context.Background(), UpdateAPIKeyInput{
		WorkspaceID: f.workspace, KeyID: created.Item.ID, ActorRole: value.RoleAdmin,
		Name: "x", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{"admin"},
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)

	// 空 KB 集。
	_, err = f.svc.Update(context.Background(), UpdateAPIKeyInput{
		WorkspaceID: f.workspace, KeyID: created.Item.ID, ActorRole: value.RoleAdmin,
		Name: "x", KnowledgeBaseIDs: nil, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)
}

func TestAPIKeyServiceUpdateRejectsKnowledgeBaseNotInWorkspace(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "missing-kb", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	// 该 KB 未在 fakeAPIKeyNameStore 注册，视为不属于 workspace。
	unknownKB := uuid.New()
	_, err = f.svc.Update(context.Background(), UpdateAPIKeyInput{
		WorkspaceID: f.workspace, KeyID: created.Item.ID, ActorRole: value.RoleAdmin,
		Name: "x", KnowledgeBaseIDs: []uuid.UUID{unknownKB}, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)
}

func TestAPIKeyServiceUpdateRejectsRevokedKey(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "to-revoke", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.Revoke(context.Background(), f.workspace, f.adminID, value.RoleAdmin, created.Item.ID))

	_, err = f.svc.Update(context.Background(), UpdateAPIKeyInput{
		WorkspaceID: f.workspace, KeyID: created.Item.ID, ActorRole: value.RoleAdmin,
		Name: "x", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.ErrorIs(t, err, domainerrors.ErrAPIKeyImmutable)
}

func TestAPIKeyServiceAuthenticateSuccessAndLastUsed(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "auth", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	require.Equal(t, 0, f.store.touchCalls)

	principal, err := f.svc.Authenticate(context.Background(), created.APIKey)
	require.NoError(t, err)
	require.True(t, principal.IsAPIKey())
	require.Equal(t, f.workspace, principal.WorkspaceID)
	require.ElementsMatch(t, f.kbIDs, principal.KnowledgeBaseIDs)
	require.Equal(t, 1, f.store.touchCalls)
}

func TestAPIKeyServiceAuthenticateRejectsRevokedExpiredAndBadFormat(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "auth", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)

	// 吊销后鉴权失败。
	require.NoError(t, f.svc.Revoke(context.Background(), f.workspace, f.adminID, value.RoleAdmin, created.Item.ID))
	_, err = f.svc.Authenticate(context.Background(), created.APIKey)
	require.ErrorIs(t, err, domainerrors.ErrUnauthorized)

	// 过期 key 鉴权失败。
	expiredKey := f.createExpiredKey(t)
	_, err = f.svc.Authenticate(context.Background(), expiredKey)
	require.ErrorIs(t, err, domainerrors.ErrUnauthorized)

	// 格式错误。
	_, err = f.svc.Authenticate(context.Background(), "not-a-key")
	require.ErrorIs(t, err, domainerrors.ErrUnauthorized)
}

func TestAPIKeyServiceRevealRejectsBadPlaintextConsistency(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "reveal", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	// 篡改密文使其解密出错误明文。
	f.store.ciphertexts[created.Item.ID] = append([]byte{0x01}, []byte("lhk_"+strings.Repeat("z", 43))...)
	_, err = f.svc.Reveal(context.Background(), f.workspace, value.RoleAdmin, created.Item.ID)
	require.ErrorIs(t, err, domainerrors.ErrAPIKeySecretUnavailable)
}

func TestAPIKeyServiceRevealRejectsDecryptionFailure(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "reveal", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	f.cipher.failDecrypt = true
	_, err = f.svc.Reveal(context.Background(), f.workspace, value.RoleAdmin, created.Item.ID)
	require.ErrorIs(t, err, domainerrors.ErrAPIKeySecretUnavailable)
}

func TestAPIKeyServiceAuthenticateDoesNotCallDecrypt(t *testing.T) {
	f := newAPIKeyFixture(t)
	created, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "no-decrypt", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	f.cipher.decryptCalls = 0
	_, err = f.svc.Authenticate(context.Background(), created.APIKey)
	require.NoError(t, err)
	require.Zero(t, f.cipher.decryptCalls, "鉴权路径不应调用解密")
}

func TestAPIKeyServiceListReturnsSafeItems(t *testing.T) {
	f := newAPIKeyFixture(t)
	_, err := f.svc.Create(context.Background(), CreateAPIKeyInput{
		WorkspaceID: f.workspace, ActorID: f.adminID, ActorRole: value.RoleAdmin,
		Name: "first", KnowledgeBaseIDs: f.kbIDs, Scopes: []value.APIScope{value.ScopeSearchRead},
	})
	require.NoError(t, err)
	items, err := f.svc.List(context.Background(), f.workspace, value.RoleAdmin)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotEmpty(t, items[0].TokenPrefix)
	require.Len(t, items[0].KnowledgeBases, 2)
}

// createExpiredKey 直接构造一个已过期的 key 存入 store，返回其明文。
func (f *apiKeyFixture) createExpiredKey(t *testing.T) string {
	t.Helper()
	material, err := GenerateAPIKeyMaterial(bytes.NewReader(bytes.Repeat([]byte{0x09}, 32)))
	require.NoError(t, err)
	past := f.now.Add(-time.Hour)
	key := &model.WorkspaceAPIKey{
		ID: uuid.New(), WorkspaceID: f.workspace, Name: "expired",
		TokenHash: material.Hash, TokenPrefix: material.Prefix,
		Scopes:           []value.APIScope{value.ScopeSearchRead},
		KnowledgeBaseIDs: f.kbIDs,
		ExpiresAt:        &past,
	}
	f.store.keys[key.ID] = key
	return material.Plaintext
}

func TestNormalizeAPIScopesSortsAndDedupes(t *testing.T) {
	out, err := normalizeAPIScopes([]value.APIScope{value.ScopeSearchRead, value.ScopeDocumentsRead, value.ScopeSearchRead})
	require.NoError(t, err)
	require.Equal(t, []value.APIScope{value.ScopeDocumentsRead, value.ScopeSearchRead}, out)
}

func TestResolveExpirationRejectsTooManyDays(t *testing.T) {
	f := newAPIKeyFixture(t)
	_, err := f.svc.resolveExpiration(APIKeyExpiration{Type: ExpirationDays, Days: 9999})
	require.ErrorIs(t, err, domainerrors.ErrValidation)
}

func TestResolveExpirationNeverReturnsNil(t *testing.T) {
	f := newAPIKeyFixture(t)
	got, err := f.svc.resolveExpiration(APIKeyExpiration{Type: ExpirationNever})
	require.NoError(t, err)
	require.Nil(t, got)
}

var _ = errors.Is
