//go:build integration

package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// sha256HexForTest 计算明文的 SHA-256 lowercase hex，用于生成合法 token_hash。
func sha256HexForTest(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// repeatStr 返回 s 重复 n 次的字符串，便于构造固定长度明文。
func repeatStr(s string, n int) string { return strings.Repeat(s, n) }

// workspaceAPIKeySeed 包含创建 API Key 所需的全部 FK 父对象 ID。
type workspaceAPIKeySeed struct {
	workspaceID uuid.UUID
	userID      uuid.UUID
	kbIDs       []uuid.UUID
}

// seedWorkspaceUserAndKnowledgeBases 插入 workspace、user 和 n 个知识库，
// 返回创建 API Key 所需的 FK 父对象。使用 openIntegrationTestDB 以便
// WorkspaceTxRunner 能在自有事务中工作（每个测试使用独立克隆库）。
func seedWorkspaceUserAndKnowledgeBases(t *testing.T, ctx context.Context, db *gorm.DB, n int) workspaceAPIKeySeed {
	t.Helper()
	workspaceID := uuid.New()
	userID := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.WithContext(ctx).Exec(
		"INSERT INTO workspaces (id, name, slug) VALUES (?, ?, ?)",
		workspaceID, "ws-"+uuid.NewString(), "ws-"+uuid.NewString(),
	).Error)
	require.NoError(t, db.WithContext(ctx).Exec(
		"INSERT INTO users (id, email, nickname, password_hash) VALUES (?, ?, ?, 'hash')",
		userID, uuid.NewString()+"@example.com", "actor",
	).Error)
	kbIDs := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		kbID := uuid.New()
		rootID := uuid.New()
		genID := uuid.New()
		providerID := uuid.New()
		modelID := uuid.New()
		// knowledge_bases 与 file_tree_nodes 之间存在 DEFERRABLE 循环外键，
		// 必须在同一事务内按 KB -> file_tree_nodes -> generation 顺序插入，
		// KB 指向 root/generation 的外键在提交时检查。
		require.NoError(t, db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(
				"INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) "+
					"VALUES (?, 'workspace', ?, ?, 'openai', 'active', ?)",
				providerID, workspaceID, "provider-"+uuid.NewString(), userID,
			).Error; err != nil {
				return err
			}
			if err := tx.Exec(
				"INSERT INTO models (id, provider_id, name, type, model_name, dimensions, status, created_by) "+
					"VALUES (?, ?, ?, 'embedding', 'text-embedding', 1024, 'active', ?)",
				modelID, providerID, "model-"+uuid.NewString(), userID,
			).Error; err != nil {
				return err
			}
			if err := tx.Exec(
				"INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id, created_at, updated_at) "+
					"VALUES (?, ?, ?, ?, ?, ?, ?)",
				kbID, workspaceID, "kb-"+uuid.NewString(), genID, rootID, now, now,
			).Error; err != nil {
				return err
			}
			if err := tx.Exec(
				"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) "+
					"VALUES (?, ?, ?, 'root', '')",
				rootID, workspaceID, kbID,
			).Error; err != nil {
				return err
			}
			return tx.Exec(
				"INSERT INTO knowledge_base_index_generations "+
					"(id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, "+
					"embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status) "+
					"VALUES (?, ?, ?, ?, ?, 'text-embedding', 1024, 'hash', 1, '{}'::jsonb, 'chash', 'ready')",
				genID, workspaceID, kbID, modelID, providerID,
			).Error
		}))
		kbIDs = append(kbIDs, kbID)
	}
	return workspaceAPIKeySeed{workspaceID: workspaceID, userID: userID, kbIDs: kbIDs}
}

func newWorkspaceAPIKeyDomain(seed workspaceAPIKeySeed, hash string) *model.WorkspaceAPIKey {
	now := time.Now().UTC()
	return &model.WorkspaceAPIKey{
		ID:          uuid.New(),
		WorkspaceID: seed.workspaceID,
		Name:        "检索 Agent",
		TokenHash:   hash,
		TokenPrefix: "lhk_a1b2c3d4",
		Scopes:      []value.APIScope{value.ScopeDocumentsRead, value.ScopeSearchRead},
		CreatedBy:   &seed.userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestWorkspaceAPIKeyRepositorySeparatesAuthAndRevealReads(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 2)

	hash := sha256HexForTest("lhk_" + repeatStr("a", 43))
	key := newWorkspaceAPIKeyDomain(seed, hash)
	require.NoError(t, repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
		return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
			Key:              key,
			SecretCiphertext: []byte{1, 2, 3},
			KnowledgeBaseIDs: seed.kbIDs,
		})
	}))

	// 鉴权 lookup 返回绑定，不读取密文。
	authenticated, err := repo.FindByTokenHashWithBindings(ctx, hash)
	require.NoError(t, err)
	require.ElementsMatch(t, seed.kbIDs, authenticated.KnowledgeBaseIDs)
	require.Equal(t, seed.workspaceID, authenticated.WorkspaceID)

	// reveal 读取专用密文。
	ciphertext, err := repo.RevealSecretCiphertext(ctx, seed.workspaceID, key.ID)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, ciphertext)
}

func TestWorkspaceAPIKeyRepositoryCreateFailsOnUnknownKnowledgeBase(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 1)
	unknownKB := uuid.New() // 不属于本 workspace

	hash := sha256HexForTest("lhk_" + repeatStr("b", 43))
	key := newWorkspaceAPIKeyDomain(seed, hash)
	err := repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
		return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
			Key:              key,
			SecretCiphertext: []byte{1, 2, 3},
			KnowledgeBaseIDs: []uuid.UUID{unknownKB},
		})
	})
	require.Error(t, err, "未知知识库应触发复合外键失败并回滚")

	// 整单回滚：key 不应存在。
	_, err = repo.Get(ctx, seed.workspaceID, key.ID)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
}

func TestWorkspaceAPIKeyRepositoryCreateFailsOnDuplicateHash(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 1)
	hash := sha256HexForTest("lhk_" + repeatStr("c", 43))

	create := func(name string) error {
		key := newWorkspaceAPIKeyDomain(seed, hash)
		key.Name = name
		return repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
			return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
				Key: key, SecretCiphertext: []byte{1}, KnowledgeBaseIDs: seed.kbIDs,
			})
		})
	}
	require.NoError(t, create("first"))
	require.ErrorIs(t, create("second"), domainerrors.ErrConflict)
}

func TestWorkspaceAPIKeyRepositoryCrossWorkspaceGetReturnsNotFound(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 1)
	hash := sha256HexForTest("lhk_" + repeatStr("d", 43))
	key := newWorkspaceAPIKeyDomain(seed, hash)
	require.NoError(t, repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
		return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
			Key: key, SecretCiphertext: []byte{1}, KnowledgeBaseIDs: seed.kbIDs,
		})
	}))

	// 另一个 workspace 读不到。
	other := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 0)
	_, err := repo.Get(ctx, other.workspaceID, key.ID)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
	_, err = repo.RevealSecretCiphertext(ctx, other.workspaceID, key.ID)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
}

func TestWorkspaceAPIKeyRepositoryRevokeIsIdempotent(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 1)
	hash := sha256HexForTest("lhk_" + repeatStr("e", 43))
	key := newWorkspaceAPIKeyDomain(seed, hash)
	require.NoError(t, repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
		return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
			Key: key, SecretCiphertext: []byte{1}, KnowledgeBaseIDs: seed.kbIDs,
		})
	}))
	now := time.Now().UTC()
	require.NoError(t, repo.Revoke(ctx, seed.workspaceID, key.ID, seed.userID, now))
	// 再次吊销仍成功（幂等）。
	require.NoError(t, repo.Revoke(ctx, seed.workspaceID, key.ID, seed.userID, now))
	got, err := repo.Get(ctx, seed.workspaceID, key.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
}

func TestWorkspaceAPIKeyRepositoryListOrderAndCountActive(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 1)
	for i := 0; i < 3; i++ {
		hash := sha256HexForTest("lhk_" + repeatStr(string(rune('f'+i)), 43))
		key := newWorkspaceAPIKeyDomain(seed, hash)
		require.NoError(t, repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
			return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
				Key: key, SecretCiphertext: []byte{1}, KnowledgeBaseIDs: seed.kbIDs,
			})
		}))
	}
	keys, err := repo.List(ctx, seed.workspaceID)
	require.NoError(t, err)
	require.Len(t, keys, 3)
	// created_at DESC 排序稳定。
	for i := 1; i < len(keys); i++ {
		require.False(t, keys[i].CreatedAt.After(keys[i-1].CreatedAt), "list should be DESC by created_at")
	}
	active, err := repo.CountActive(ctx, seed.workspaceID, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 3, active)
}

func TestWorkspaceAPIKeyRepositoryTouchLastUsedThrottles(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	repo := NewWorkspaceAPIKeyRepository(gormDB)
	seed := seedWorkspaceUserAndKnowledgeBases(t, ctx, gormDB, 1)
	hash := sha256HexForTest("lhk_" + repeatStr("g", 43))
	key := newWorkspaceAPIKeyDomain(seed, hash)
	require.NoError(t, repo.WithinWorkspace(ctx, seed.workspaceID, func(ctx context.Context, tx service.WorkspaceAPIKeyTx) error {
		return tx.CreateWithKnowledgeBaseBindings(ctx, service.WorkspaceAPIKeyCreateRecord{
			Key: key, SecretCiphertext: []byte{1}, KnowledgeBaseIDs: seed.kbIDs,
		})
	}))
	now := time.Now().UTC()
	// 首次更新 last_used_at。
	require.NoError(t, repo.TouchLastUsed(ctx, seed.workspaceID, key.ID, now, 5*time.Minute))
	got, err := repo.Get(ctx, seed.workspaceID, key.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	require.WithinDuration(t, now, *got.LastUsedAt, time.Second)
	// 5 分钟内不应更新。
	require.NoError(t, repo.TouchLastUsed(ctx, seed.workspaceID, key.ID, now.Add(1*time.Minute), 5*time.Minute))
	got2, err := repo.Get(ctx, seed.workspaceID, key.ID)
	require.NoError(t, err)
	require.Equal(t, got.LastUsedAt, got2.LastUsedAt)
}
