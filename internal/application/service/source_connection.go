package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"

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

// SourceConnectionService 管理工作区级外部内容源连接（如飞书内部应用）。
type SourceConnectionService struct {
	repo   SourceConnectionRepository
	cipher SourceConnectionCipher
	now    func() time.Time
}

// SourceConnectionServiceDeps 注入依赖。
type SourceConnectionServiceDeps struct {
	Repository SourceConnectionRepository
	Cipher     SourceConnectionCipher
}

func NewSourceConnectionService(deps SourceConnectionServiceDeps) *SourceConnectionService {
	return &SourceConnectionService{repo: deps.Repository, cipher: deps.Cipher, now: time.Now}
}

// CreateSourceConnectionInput 创建连接的输入；AppSecret 为明文，加密后落库。
type CreateSourceConnectionInput struct {
	WorkspaceID uuid.UUID
	Provider    string
	Name        string
	AppID       string
	AppSecret   string
}

// Create 创建连接：先落库拿 ID，再加密 secret 回写。
// 这里用先创建后回填密文的两步，避免加密时 AAD 依赖的 ID 未生成。
func (s *SourceConnectionService) Create(ctx context.Context, input CreateSourceConnectionInput) (*dto.SourceConnection, error) {
	if input.AppSecret == "" {
		return nil, fmt.Errorf("%w: app_secret 不能为空", domainerrors.ErrValidation)
	}
	conn, err := model.NewSourceConnection(model.NewSourceConnectionInput{
		WorkspaceID:           input.WorkspaceID,
		Provider:              input.Provider,
		Name:                  input.Name,
		AppID:                 input.AppID,
		CredentialsCiphertext: placeholderCipher, // 占位，落库后回填真实密文
	})
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.cipher.Encrypt(conn.ID, []byte(input.AppSecret))
	if err != nil {
		return nil, fmt.Errorf("加密来源连接凭证失败: %w", err)
	}
	conn.CredentialsCiphertext = ciphertext
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

// placeholderCipher 是落库前的占位密文，Create 后立即回填真实密文。
// 非空以满足 model.NewSourceConnection 的非空校验。
var placeholderCipher = []byte{0x01}

func toConnectionDTO(conn *model.SourceConnection) *dto.SourceConnection {
	appID, _ := conn.Config["app_id"].(string)
	return &dto.SourceConnection{
		ID: conn.ID, WorkspaceID: conn.WorkspaceID, Provider: conn.Provider,
		Name: conn.Name, AppID: appID, Status: conn.Status,
		CreatedAt: conn.CreatedAt, UpdatedAt: conn.UpdatedAt,
	}
}
