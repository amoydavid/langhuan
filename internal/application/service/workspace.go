package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// WorkspaceRepository 描述 workspace 聚合的仓储抽象（服务层本地接口，由 db.WorkspaceRepository 实现）。
// CreateWithOwner 在单一事务内创建 workspace 与 owner 成员关系，使事务边界
// 落在持有 *gorm.DB 的基础设施层，服务层不感知数据库句柄。
type WorkspaceRepository interface {
	Create(ctx context.Context, ws *model.Workspace) error
	Get(ctx context.Context, id uuid.UUID) (*model.Workspace, error)
	GetDefault(ctx context.Context) (*model.Workspace, error)
	GetBySlug(ctx context.Context, slug string) (*model.Workspace, error)
	CreateWithOwner(ctx context.Context, ws *model.Workspace, ownerUserID uuid.UUID) error
}

type WorkspaceService struct {
	repo WorkspaceRepository
}

type CreateWorkspaceInput struct {
	Name string
	Slug string
}

func NewWorkspaceService(repo WorkspaceRepository) *WorkspaceService {
	return &WorkspaceService{repo: repo}
}

func (s *WorkspaceService) Create(ctx context.Context, input CreateWorkspaceInput) (*dto.Workspace, error) {
	ws, err := model.NewWorkspace(input.Name, input.Slug, map[string]any{})
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, ws); err != nil {
		return nil, err
	}
	return dto.WorkspaceFromModel(ws), nil
}

// CreateForPlatformAdmin 创建 workspace 并在同一事务内为发起者建立 owner 成员关系。
// 仅 platform_admin 可调用（防御性校验：真正的鉴权由中间件完成）；
// 非管理员一律 ErrForbidden。slug 唯一冲突由事务内唯一约束保证。
func (s *WorkspaceService) CreateForPlatformAdmin(ctx context.Context, input CreateWorkspaceInput, creatorUserID uuid.UUID, creatorIsPlatformAdmin bool) (*dto.Workspace, error) {
	if !creatorIsPlatformAdmin {
		return nil, domainerrors.ErrForbidden
	}
	ws, err := model.NewWorkspace(input.Name, input.Slug, map[string]any{})
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateWithOwner(ctx, ws, creatorUserID); err != nil {
		return nil, err
	}
	return dto.WorkspaceFromModel(ws), nil
}

func (s *WorkspaceService) Get(ctx context.Context, id uuid.UUID) (*dto.Workspace, error) {
	ws, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.WorkspaceFromModel(ws), nil
}

// GetBySlug 按 slug 查询 workspace。未找到返回 ErrNotFound。
func (s *WorkspaceService) GetBySlug(ctx context.Context, slug string) (*dto.Workspace, error) {
	ws, err := s.repo.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return dto.WorkspaceFromModel(ws), nil
}

func (s *WorkspaceService) GetDefault(ctx context.Context) (*dto.Workspace, error) {
	ws, err := s.repo.GetDefault(ctx)
	if err != nil {
		return nil, err
	}
	return dto.WorkspaceFromModel(ws), nil
}
