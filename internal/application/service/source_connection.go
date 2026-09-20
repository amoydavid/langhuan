package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"

	"github.com/dajee/langhuan/internal/application/dto"
)

// SourceConnectionRepository 是 SourceConnectionService 消费的持久化端口。
type SourceConnectionRepository interface {
	Create(ctx context.Context, conn *model.SourceConnection) error
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*model.SourceConnection, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]*model.SourceConnection, error)
	Update(ctx context.Context, conn *model.SourceConnection) error
	SoftDelete(ctx context.Context, workspaceID, id uuid.UUID) error
}

// SourceConnectionCipher 加解密来源连接凭证（AAD 绑定连接 ID）。
type SourceConnectionCipher interface {
	Encrypt(connectionID uuid.UUID, plaintext []byte) ([]byte, error)
	Decrypt(connectionID uuid.UUID, ciphertext []byte) ([]byte, error)
}

// SourceConnectionProber 对单个连接做非持久化的连通性探测（spec §9.4 连接测试）。
// 实现在 adapters/source/web（firecrawl/jina/browser 三 provider）；测试经注入
// fake 覆盖，不发真实网络请求。失败以 error 表达，成功路径无错误。
type SourceConnectionProber interface {
	Probe(ctx context.Context, provider value.SourceProvider, secret, endpointURL string) error
}

// SourceConnectionService 管理工作区级外部内容源连接（如飞书内部应用）。
type SourceConnectionService struct {
	repo   SourceConnectionRepository
	cipher SourceConnectionCipher
	// selector 供连接测试复用凭证解密路径（spec §9.4）；CRUD 路径不消费。
	// prober 承载 firecrawl/jina/browser 的连通性探测；feishu 无测试。
	selector *SourceConnectionSelector
	prober   SourceConnectionProber
	now      func() time.Time
}

// SourceConnectionServiceDeps 注入依赖。
type SourceConnectionServiceDeps struct {
	Repository SourceConnectionRepository
	Cipher     SourceConnectionCipher
	// Selector / Prober 仅连接测试消费；缺失时 TestConnection 返回装配错误。
	Selector *SourceConnectionSelector
	Prober   SourceConnectionProber
}

func NewSourceConnectionService(deps SourceConnectionServiceDeps) *SourceConnectionService {
	return &SourceConnectionService{
		repo:     deps.Repository,
		cipher:   deps.Cipher,
		selector: deps.Selector,
		prober:   deps.Prober,
		now:      time.Now,
	}
}

// CreateSourceConnectionInput 创建连接的输入；AppSecret 为明文，加密后落库。
// Config 承载 provider 相关的非敏感配置（firecrawl endpoint 覆盖、browser CDP
// endpoint_url，键名统一 endpoint_url——T11 凭证注入按此键消费）。
type CreateSourceConnectionInput struct {
	WorkspaceID uuid.UUID
	Provider    string
	Name        string
	AppID       string
	AppSecret   string
	Config      map[string]any
}

// Create 创建连接：先落库拿 ID，再加密 secret 回填。
// 这里用先创建后回填密文的两步，避免加密时 AAD 依赖的 ID 未生成。
//
// 凭证按 provider 分支（spec §3.1）：feishu/firecrawl/jina 必填 app_secret；
// browser 允许空凭证（token 可选），空凭证落库为空密文、不触发加密。
func (s *SourceConnectionService) Create(ctx context.Context, input CreateSourceConnectionInput) (*dto.SourceConnection, error) {
	provider := value.SourceProvider(strings.ToLower(strings.TrimSpace(input.Provider)))
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	if provider.RequiresCredentials() && input.AppSecret == "" {
		return nil, fmt.Errorf("%w: app_secret 不能为空", domainerrors.ErrValidation)
	}
	conn, err := model.NewSourceConnection(model.NewSourceConnectionInput{
		WorkspaceID:           input.WorkspaceID,
		Provider:              input.Provider,
		Name:                  input.Name,
		AppID:                 input.AppID,
		Config:                input.Config,
		CredentialsCiphertext: placeholderCipher, // 占位，落库后回填真实密文
	})
	if err != nil {
		return nil, err
	}
	if input.AppSecret != "" {
		ciphertext, err := s.cipher.Encrypt(conn.ID, []byte(input.AppSecret))
		if err != nil {
			return nil, fmt.Errorf("加密来源连接凭证失败: %w", err)
		}
		conn.CredentialsCiphertext = ciphertext
	} else {
		// browser 无 token：空密文，selector 对空密文按空凭证处理（不调用 Decrypt）。
		conn.CredentialsCiphertext = nil
	}
	if err := s.repo.Create(ctx, conn); err != nil {
		return nil, err
	}
	return toConnectionDTO(conn), nil
}

// List 列出工作区下所有连接，不返回 secret 明文。
func (s *SourceConnectionService) List(ctx context.Context, workspaceID uuid.UUID) ([]dto.SourceConnection, error) {
	conns, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.SourceConnection, len(conns))
	for i, conn := range conns {
		result[i] = *toConnectionDTO(conn)
	}
	return result, nil
}

// Get 读取单条连接（不含 secret）。
func (s *SourceConnectionService) Get(ctx context.Context, workspaceID, id uuid.UUID) (*dto.SourceConnection, error) {
	conn, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return toConnectionDTO(conn), nil
}

// UpdateSourceConnectionInput 更新连接；AppSecret 非空时轮换凭证。
type UpdateSourceConnectionInput struct {
	WorkspaceID uuid.UUID
	ID          uuid.UUID
	Name        *string
	Status      *string
	AppSecret   *string
}

// Update 更新连接的非敏感字段，或在 AppSecret 非空时轮换凭证密文。
func (s *SourceConnectionService) Update(ctx context.Context, input UpdateSourceConnectionInput) (*dto.SourceConnection, error) {
	conn, err := s.repo.Get(ctx, input.WorkspaceID, input.ID)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		conn.Name = *input.Name
	}
	if input.Status != nil {
		conn.Status = *input.Status
	}
	if input.AppSecret != nil && *input.AppSecret != "" {
		ciphertext, err := s.cipher.Encrypt(conn.ID, []byte(*input.AppSecret))
		if err != nil {
			return nil, fmt.Errorf("加密来源连接凭证失败: %w", err)
		}
		conn.CredentialsCiphertext = ciphertext
	}
	conn.UpdatedAt = s.now()
	if err := s.repo.Update(ctx, conn); err != nil {
		return nil, err
	}
	return toConnectionDTO(conn), nil
}

// Delete 软删连接。
func (s *SourceConnectionService) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	return s.repo.SoftDelete(ctx, workspaceID, id)
}

// TestConnection 对已保存的连接做一次非持久化连通性探测（spec §9.4）：
// 凭证复用 selector 解密路径在调用时解密；结果不落库。探测成功/失败都以
// 结果表达（失败原因在 Message，绝不包含凭证），error 仅表达「无法测试」——
// 连接不存在（ErrNotFound）、已停用（ErrValidation，启用后可重试）、解密失败
// 或 provider 无测试能力（feishu，ErrValidation）。
//
// 契约取舍（T12 评审移交 b，与既有模型连接测试不同，勿强行统一）：
//   - 本端点（POST .../source-connections/:connection_id/test）：探测失败是
//     「结果」而非「错误」→ HTTP 200 + {ok:false, message:原因}；
//   - 模型连接测试（POST .../models/:model_id/test）：失败即 error →
//     4xx/5xx + {error:{code,message}}（如 connection_test_failed→502）。
//     前端（T17）消费本端点时按 ok 字段分支，不按状态码判失败。
func (s *SourceConnectionService) TestConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) (*dto.SourceConnectionTestResult, error) {
	if s.selector == nil || s.prober == nil {
		return nil, fmt.Errorf("%w: 连接测试依赖未配置", domainerrors.ErrValidation)
	}
	selected, err := s.selector.Select(ctx, workspaceID, connectionID)
	if err != nil {
		if errors.Is(err, ErrSourceConnectionDisabled) {
			// 停用是用户可自助解决的状态：包装为 ErrValidation 映射 400，
			// 避免裸错误落入 500 internal_error 被当内部故障记日志（T12 评审移交 a）。
			return nil, fmt.Errorf("%w: 来源连接 %s 已停用，请先启用后再测试", domainerrors.ErrValidation, connectionID)
		}
		return nil, err
	}
	provider := value.SourceProvider(strings.TrimSpace(selected.Provider))
	switch provider {
	case value.SourceProviderFirecrawl, value.SourceProviderJina, value.SourceProviderBrowser:
	default:
		return nil, fmt.Errorf("%w: %s 连接不支持连接测试", domainerrors.ErrValidation, provider)
	}
	endpointURL, _ := selected.Config[model.SourceConnectionConfigKeyEndpointURL].(string)
	started := time.Now()
	if err := s.prober.Probe(ctx, provider, string(selected.AppSecret), strings.TrimSpace(endpointURL)); err != nil {
		return &dto.SourceConnectionTestResult{
			OK:         false,
			Provider:   string(provider),
			DurationMS: time.Since(started).Milliseconds(),
			Message:    err.Error(),
		}, nil
	}
	duration := time.Since(started)
	return &dto.SourceConnectionTestResult{
		OK:         true,
		Provider:   string(provider),
		DurationMS: duration.Milliseconds(),
		Message:    fmt.Sprintf("连接成功（%s，耗时 %d ms）", provider, duration.Milliseconds()),
	}, nil
}

// placeholderCipher 是落库前的占位密文，Create 后立即回填真实密文。
// 非空以满足 model.NewSourceConnection 的非空校验。
var placeholderCipher = []byte{0x01}

func toConnectionDTO(conn *model.SourceConnection) *dto.SourceConnection {
	appID, _ := conn.Config["app_id"].(string)
	endpointURL, _ := conn.Config[model.SourceConnectionConfigKeyEndpointURL].(string)
	return &dto.SourceConnection{
		ID: conn.ID, WorkspaceID: conn.WorkspaceID, Provider: conn.Provider,
		Name: conn.Name, AppID: appID, EndpointURL: endpointURL, Status: conn.Status,
		CreatedAt: conn.CreatedAt, UpdatedAt: conn.UpdatedAt,
	}
}
