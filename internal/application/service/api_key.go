package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// APIKeyNameStore 提供 API Key 摘要所需的可读名称解析（知识库与创建者）。
// 接口定义在使用方，由基础设施层组合现有 Repository 实现。
type APIKeyNameStore interface {
	// KnowledgeBaseNames 返回给定 workspace 下指定知识库 ID 的 (id, name) 映射。
	KnowledgeBaseNames(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
	// UserNickname 返回给定用户 ID 的昵称；不存在返回空串。
	UserNickname(ctx context.Context, userID uuid.UUID) (string, error)
}

// APIKeyService 编排 Workspace API Key 的创建、列表、详情、Reveal、吊销与鉴权。
//
// 创建顺序固定为：生成 key ID 与 secret -> hash -> 用 ID/Workspace 组成 AAD
// 加密 -> transaction 验证所有知识库 -> 保存 key、密文、绑定。任何加密或持久化
// 失败都不返回明文成功响应。鉴权只使用 hash，绝不解密。
type APIKeyService struct {
	store                 WorkspaceAPIKeyStore
	cipher                APIKeySecretCipher
	names                 APIKeyNameStore
	urls                  *PublicURLBuilder
	random                io.Reader
	now                   func() time.Time
	logger                *slog.Logger
	defaultLifetime       time.Duration
	maxLifetime           time.Duration
	lastUsedTouchInterval time.Duration
	activeLimit           int
}

// APIKeyServiceDeps 注入 APIKeyService 的全部依赖。
type APIKeyServiceDeps struct {
	Store  WorkspaceAPIKeyStore
	Cipher APIKeySecretCipher
	Names  APIKeyNameStore
	URLs   *PublicURLBuilder
	Config config.APIKeyConfig
	// Random 为空时使用 crypto/rand.Reader。
	Random io.Reader
	// Now 为空时使用 time.Now。
	Now    func() time.Time
	Logger *slog.Logger
}

// NewAPIKeyService 构造 APIKeyService。随机源与 Now 缺省时使用生产实现。
func NewAPIKeyService(deps APIKeyServiceDeps) (*APIKeyService, error) {
	if deps.Store == nil || deps.Cipher == nil || deps.Names == nil || deps.URLs == nil {
		return nil, fmt.Errorf("%w: APIKeyService 依赖不能为空", domainerrors.ErrValidation)
	}
	if deps.Config.DefaultLifetimeSeconds <= 0 || deps.Config.MaxLifetimeSeconds <= 0 || deps.Config.ActiveLimit <= 0 {
		return nil, fmt.Errorf("%w: APIKeyConfig 参数必须为正", domainerrors.ErrValidation)
	}
	random := deps.Random
	if random == nil {
		random = rand.Reader
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &APIKeyService{
		store:                 deps.Store,
		cipher:                deps.Cipher,
		names:                 deps.Names,
		urls:                  deps.URLs,
		random:                random,
		now:                   now,
		logger:                logger,
		defaultLifetime:       time.Duration(deps.Config.DefaultLifetimeSeconds) * time.Second,
		maxLifetime:           time.Duration(deps.Config.MaxLifetimeSeconds) * time.Second,
		lastUsedTouchInterval: time.Duration(deps.Config.LastUsedTouchIntervalSeconds) * time.Second,
		activeLimit:           deps.Config.ActiveLimit,
	}, nil
}

// CreateAPIKeyResult 是创建 API Key 的响应，包含一次性明文与安全 item。
type CreateAPIKeyResult struct {
	APIKey string
	Item   dto.WorkspaceAPIKey
	URLs   dto.PublicURLs
}

// Create 在权限校验、上限校验与去重后，于一个 Workspace 事务内原子创建 key、
// 密文与绑定。默认到期 90 天，type=never 为不限期。
func (s *APIKeyService) Create(ctx context.Context, input CreateAPIKeyInput) (*CreateAPIKeyResult, error) {
	if err := s.requireManagerRole(input.ActorRole); err != nil {
		return nil, err
	}
	if err := validateAPIKeyName(input.Name); err != nil {
		return nil, err
	}
	scopes, err := normalizeAPIScopes(input.Scopes)
	if err != nil {
		return nil, err
	}
	kbIDs, err := dedupeKnowledgeBaseIDs(input.KnowledgeBaseIDs)
	if err != nil {
		return nil, err
	}
	expiresAt, err := s.resolveExpiration(input.Expiration)
	if err != nil {
		return nil, err
	}

	activeCount, err := s.store.CountActive(ctx, input.WorkspaceID, s.now())
	if err != nil {
		return nil, fmt.Errorf("统计活跃 API Key 失败: %w", err)
	}
	if activeCount >= s.activeLimit {
		return nil, domainerrors.ErrAPIKeyLimitReached
	}

	material, err := GenerateAPIKeyMaterial(s.random)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	keyID := uuid.New()
	key := &model.WorkspaceAPIKey{
		ID:          keyID,
		WorkspaceID: input.WorkspaceID,
		Name:        normalizeAPIKeyName(input.Name),
		TokenHash:   material.Hash,
		TokenPrefix: material.Prefix,
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
		CreatedBy:   nullableActor(input.ActorID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ciphertext, err := s.cipher.Encrypt(input.WorkspaceID, keyID, []byte(material.Plaintext))
	if err != nil {
		return nil, fmt.Errorf("加密 API Key 明文失败: %w", err)
	}
	if err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(ctx context.Context, tx WorkspaceAPIKeyTx) error {
		return tx.CreateWithKnowledgeBaseBindings(ctx, WorkspaceAPIKeyCreateRecord{
			Key:              key,
			SecretCiphertext: ciphertext,
			KnowledgeBaseIDs: kbIDs,
		})
	}); err != nil {
		return nil, err
	}

	item, err := s.toItem(ctx, key)
	if err != nil {
		return nil, err
	}
	return &CreateAPIKeyResult{
		APIKey: material.Plaintext,
		Item:   item,
		URLs:   s.urls.URLs(),
	}, nil
}

// Update 修改 API Key 的名称、知识库集合、scopes 与过期时间。归一化语义与 Create 一致，
// 已吊销的 key 视为终态禁止修改（由 store 的 revoked_at IS NULL 条件兜底，避免 TOCTOU）。
func (s *APIKeyService) Update(ctx context.Context, input UpdateAPIKeyInput) (dto.WorkspaceAPIKey, error) {
	if err := s.requireManagerRole(input.ActorRole); err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	if err := validateAPIKeyName(input.Name); err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	scopes, err := normalizeAPIScopes(input.Scopes)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	kbIDs, err := dedupeKnowledgeBaseIDs(input.KnowledgeBaseIDs)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	expiresAt, err := s.resolveExpiration(input.Expiration)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	// 应用层 KB 存在性校验：缺失任意 KB 即该 KB 不属于 workspace，返回清晰错误而非依赖 DB FK。
	kbNames, err := s.names.KnowledgeBaseNames(ctx, input.WorkspaceID, kbIDs)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	for _, id := range kbIDs {
		if _, ok := kbNames[id]; !ok {
			return dto.WorkspaceAPIKey{}, fmt.Errorf("%w: 知识库 %s 不存在或无权访问", domainerrors.ErrValidation, id)
		}
	}
	now := s.now().UTC()
	if err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(ctx context.Context, tx WorkspaceAPIKeyTx) error {
		return tx.UpdateKnowledgeBaseScope(ctx, input.WorkspaceID, input.KeyID, kbIDs, scopes, normalizeAPIKeyName(input.Name), expiresAt, now)
	}); err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	key, err := s.store.Get(ctx, input.WorkspaceID, input.KeyID)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	return s.toItem(ctx, key)
}

// Get 返回单条 API Key 的安全视图。
func (s *APIKeyService) Get(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole, keyID uuid.UUID) (dto.WorkspaceAPIKey, error) {
	if err := s.requireManagerRole(actorRole); err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	key, err := s.store.Get(ctx, workspaceID, keyID)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	return s.toItem(ctx, key)
}

// List 返回 Workspace 内全部 API Key 的安全视图。
func (s *APIKeyService) List(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole) ([]dto.WorkspaceAPIKey, error) {
	if err := s.requireManagerRole(actorRole); err != nil {
		return nil, err
	}
	keys, err := s.store.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	// 预加载全部知识库名称与创建者昵称，避免 N+1。
	allKBIDs := collectKnowledgeBaseIDs(keys)
	allUserIDs := collectCreatedByIDs(keys)
	kbNames, err := s.names.KnowledgeBaseNames(ctx, workspaceID, allKBIDs)
	if err != nil {
		return nil, err
	}
	userNicknames, err := s.loadNicknames(ctx, allUserIDs)
	if err != nil {
		return nil, err
	}
	items := make([]dto.WorkspaceAPIKey, 0, len(keys))
	for _, key := range keys {
		items = append(items, s.toItemFromCache(key, kbNames, userNicknames))
	}
	return items, nil
}

// RevealResult 是 Reveal 的响应，包含一次性明文与派生地址。
type RevealResult struct {
	APIKey string
	URLs   dto.PublicURLs
}

// Reveal 读取专用密文，解密后重新校验格式、prefix 与 hash；不一致映射安全错误。
// active/expiring/expired/revoked 都可 reveal。
func (s *APIKeyService) Reveal(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole, keyID uuid.UUID) (*RevealResult, error) {
	if err := s.requireManagerRole(actorRole); err != nil {
		return nil, err
	}
	key, err := s.store.Get(ctx, workspaceID, keyID)
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.store.RevealSecretCiphertext(ctx, workspaceID, keyID)
	if err != nil {
		return nil, err
	}
	plaintext, err := s.cipher.Decrypt(workspaceID, keyID, ciphertext)
	if err != nil {
		s.logger.WarnContext(ctx, "API Key 明文不可恢复", "workspace_id", workspaceID, "api_key_id", keyID)
		return nil, domainerrors.ErrAPIKeySecretUnavailable
	}
	if err := ValidateAPIKeyPlaintext(string(plaintext)); err != nil {
		return nil, domainerrors.ErrAPIKeySecretUnavailable
	}
	recomputed, err := HashAPIKey(string(plaintext))
	if err != nil || recomputed != key.TokenHash {
		return nil, domainerrors.ErrAPIKeySecretUnavailable
	}
	if string(plaintext)[:len(materialPrefix())] != materialPrefix() || string(plaintext)[:apiKeyPrefixDisplayLen] != key.TokenPrefix {
		return nil, domainerrors.ErrAPIKeySecretUnavailable
	}
	return &RevealResult{APIKey: string(plaintext), URLs: s.urls.URLs()}, nil
}

// Revoke 幂等吊销 API Key。actorID 为空表示未知 actor。
func (s *APIKeyService) Revoke(ctx context.Context, workspaceID, actorID uuid.UUID, actorRole value.WorkspaceRole, keyID uuid.UUID) error {
	if err := s.requireManagerRole(actorRole); err != nil {
		return err
	}
	now := s.now().UTC()
	if err := s.store.Revoke(ctx, workspaceID, keyID, actorID, now); err != nil {
		return err
	}
	return nil
}

// Authenticate 严格校验格式、计算 hash、按唯一 hash 查记录和绑定、判断吊销/到期、
// 确认绑定非空，再构造不含明文/hash/密文的 principal。格式错误、查无记录、已吊销、
// 已到期统一返回 ErrUnauthorized；成功后 best-effort 触碰 last_used_at。
func (s *APIKeyService) Authenticate(ctx context.Context, plaintext string) (value.AuthContext, error) {
	hash, err := HashAPIKey(plaintext)
	if err != nil {
		return value.AuthContext{}, domainerrors.ErrUnauthorized
	}
	key, err := s.store.FindByTokenHashWithBindings(ctx, hash)
	if err != nil {
		return value.AuthContext{}, domainerrors.ErrUnauthorized
	}
	if key == nil || key.RevokedAt != nil || len(key.KnowledgeBaseIDs) == 0 {
		return value.AuthContext{}, domainerrors.ErrUnauthorized
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(s.now()) {
		return value.AuthContext{}, domainerrors.ErrUnauthorized
	}
	principal := value.NewAPIKeyAuthContext(key.ID, key.WorkspaceID, key.Scopes, key.KnowledgeBaseIDs)
	if err := s.store.TouchLastUsed(ctx, key.WorkspaceID, key.ID, s.now().UTC(), s.lastUsedTouchInterval); err != nil {
		s.logger.WarnContext(ctx, "更新 API Key 最近使用时间失败", "workspace_id", key.WorkspaceID, "api_key_id", key.ID)
	}
	return principal, nil
}

// PublicURLs 暴露派生地址，供 handler 组装响应。
func (s *APIKeyService) PublicURLs() dto.PublicURLs { return s.urls.URLs() }

func (s *APIKeyService) requireManagerRole(role value.WorkspaceRole) error {
	if !role.AtLeast(value.RoleAdmin) {
		return domainerrors.ErrForbidden
	}
	return nil
}

func (s *APIKeyService) resolveExpiration(expiration APIKeyExpiration) (*time.Time, error) {
	if expiration.Type == "" || expiration.Type == ExpirationDays {
		days := expiration.Days
		if days == 0 {
			days = int(s.defaultLifetime / (24 * time.Hour))
		}
		if days < 1 {
			return nil, fmt.Errorf("%w: 到期天数必须大于 0", domainerrors.ErrValidation)
		}
		maxDays := int(s.maxLifetime / (24 * time.Hour))
		if days > maxDays {
			return nil, fmt.Errorf("%w: 到期天数不能超过 %d", domainerrors.ErrValidation, maxDays)
		}
		t := s.now().Add(time.Duration(days) * 24 * time.Hour).UTC()
		return &t, nil
	}
	if expiration.Type == ExpirationNever {
		return nil, nil
	}
	return nil, fmt.Errorf("%w: 未知到期方式 %q", domainerrors.ErrValidation, expiration.Type)
}

func (s *APIKeyService) toItem(ctx context.Context, key *model.WorkspaceAPIKey) (dto.WorkspaceAPIKey, error) {
	kbNames, err := s.names.KnowledgeBaseNames(ctx, key.WorkspaceID, key.KnowledgeBaseIDs)
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	nicknames, err := s.loadNicknames(ctx, collectCreatedByIDs([]*model.WorkspaceAPIKey{key}))
	if err != nil {
		return dto.WorkspaceAPIKey{}, err
	}
	return s.toItemFromCache(key, kbNames, nicknames), nil
}

func (s *APIKeyService) toItemFromCache(key *model.WorkspaceAPIKey, kbNames map[uuid.UUID]string, nicknames map[uuid.UUID]string) dto.WorkspaceAPIKey {
	kbs := make([]dto.WorkspaceAPIKeyKnowledgeBaseSummary, 0, len(key.KnowledgeBaseIDs))
	for _, id := range sortedKnowledgeBaseIDs(key.KnowledgeBaseIDs) {
		kbs = append(kbs, dto.WorkspaceAPIKeyKnowledgeBaseSummary{ID: id, Name: kbNames[id]})
	}
	item := dto.WorkspaceAPIKey{
		ID:             key.ID,
		Name:           key.Name,
		TokenPrefix:    key.TokenPrefix,
		KnowledgeBases: kbs,
		Scopes:         key.Scopes,
		Status:         value.DeriveAPIKeyStatus(key.RevokedAt, key.ExpiresAt, s.now()),
		ExpiresAt:      key.ExpiresAt,
		LastUsedAt:     key.LastUsedAt,
		RevokedAt:      key.RevokedAt,
		CreatedAt:      key.CreatedAt,
	}
	if key.CreatedBy != nil {
		item.CreatedBy = &dto.WorkspaceAPIKeyActorSummary{ID: *key.CreatedBy, Nickname: nicknames[*key.CreatedBy]}
	}
	return item
}

func (s *APIKeyService) loadNicknames(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(userIDs))
	for _, id := range userIDs {
		nickname, err := s.names.UserNickname(ctx, id)
		if err != nil {
			return nil, err
		}
		out[id] = nickname
	}
	return out, nil
}

func validateAPIKeyName(name string) error {
	trimmed := normalizeAPIKeyName(name)
	if len(trimmed) < 1 || len(trimmed) > 80 {
		return fmt.Errorf("%w: 名称长度必须在 1 到 80 之间", domainerrors.ErrValidation)
	}
	return nil
}

func normalizeAPIKeyName(name string) string {
	// 仅去首尾空白；保留内部内容。CHECK 约束使用 btrim。
	for len(name) > 0 && (name[0] == ' ' || name[0] == '\t' || name[0] == '\n' || name[0] == '\r') {
		name = name[1:]
	}
	for len(name) > 0 && (name[len(name)-1] == ' ' || name[len(name)-1] == '\t' || name[len(name)-1] == '\n' || name[len(name)-1] == '\r') {
		name = name[:len(name)-1]
	}
	return name
}

func normalizeAPIScopes(scopes []value.APIScope) ([]value.APIScope, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("%w: 至少选择一个 scope", domainerrors.ErrValidation)
	}
	seen := make(map[value.APIScope]bool, len(scopes))
	out := make([]value.APIScope, 0, len(scopes))
	for _, scope := range scopes {
		if !value.IsValidAPIScope(scope) {
			return nil, fmt.Errorf("%w: 非法 scope %q", domainerrors.ErrValidation, scope)
		}
		if !seen[scope] {
			seen[scope] = true
			out = append(out, scope)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func dedupeKnowledgeBaseIDs(ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: 至少绑定一个知识库", domainerrors.ErrValidation)
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%w: 知识库 ID 不能为空", domainerrors.ErrValidation)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func nullableActor(actorID uuid.UUID) *uuid.UUID {
	if actorID == uuid.Nil {
		return nil
	}
	return &actorID
}

func collectKnowledgeBaseIDs(keys []*model.WorkspaceAPIKey) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	out := make([]uuid.UUID, 0)
	for _, key := range keys {
		for _, id := range key.KnowledgeBaseIDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func collectCreatedByIDs(keys []*model.WorkspaceAPIKey) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	out := make([]uuid.UUID, 0)
	for _, key := range keys {
		if key.CreatedBy != nil && !seen[*key.CreatedBy] {
			seen[*key.CreatedBy] = true
			out = append(out, *key.CreatedBy)
		}
	}
	return out
}

func sortedKnowledgeBaseIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// materialPrefix 返回固定前缀，避免循环 import api_key_material 常量。
func materialPrefix() string { return apiKeyPlaintextPrefix }
