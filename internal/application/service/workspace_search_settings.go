package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// WorkspaceSearchSettingsRepository 是 Workspace 默认查询策略的持久化合同。
type WorkspaceSearchSettingsRepository interface {
	Get(context.Context, uuid.UUID) (*model.WorkspaceSearchSettings, error)
	Upsert(context.Context, *model.WorkspaceSearchSettings) error
}

// SearchProfileResolver 是搜索运行时读取 Workspace Rerank 快照的最小合同。
type SearchProfileResolver interface {
	Resolve(context.Context, uuid.UUID) (*model.RerankSnapshot, error)
}

// UpdateWorkspaceSearchSettingsInput 描述管理员提交的默认 Rerank 策略。
type UpdateWorkspaceSearchSettingsInput struct {
	RerankEnabled bool
	ModelID       uuid.UUID
	CandidateTopK int
	FailureMode   value.RerankFailureMode
	ActorID       uuid.UUID
}

// WorkspaceSearchSettingsService 管理 Workspace 默认查询阶段策略。
type WorkspaceSearchSettingsService struct {
	repo   WorkspaceSearchSettingsRepository
	models IndexGenerationModelResolver
}

// NewWorkspaceSearchSettingsService 创建 Workspace Search Settings service。
func NewWorkspaceSearchSettingsService(repo WorkspaceSearchSettingsRepository, models IndexGenerationModelResolver) *WorkspaceSearchSettingsService {
	return &WorkspaceSearchSettingsService{repo: repo, models: models}
}

// Get 返回当前策略；没有显式配置时返回关闭 Rerank 的默认值。
func (s *WorkspaceSearchSettingsService) Get(ctx context.Context, workspaceID uuid.UUID) (*dto.WorkspaceSearchSettings, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	settings, err := s.repo.Get(ctx, workspaceID)
	if errors.Is(err, domainerrors.ErrNotFound) {
		return dto.WorkspaceSearchSettingsFromModel(&model.WorkspaceSearchSettings{WorkspaceID: workspaceID}), nil
	}
	if err != nil {
		return nil, err
	}
	return dto.WorkspaceSearchSettingsFromModel(settings), nil
}

// Resolve 返回搜索时使用的不可变 Rerank 快照；没有配置时返回 nil。
func (s *WorkspaceSearchSettingsService) Resolve(ctx context.Context, workspaceID uuid.UUID) (*model.RerankSnapshot, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	settings, err := s.repo.Get(ctx, workspaceID)
	if errors.Is(err, domainerrors.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if settings == nil || settings.Rerank == nil {
		return nil, nil
	}
	return settings.Rerank.Clone(), nil
}

// Update 只允许 Workspace owner/admin 修改默认查询策略。
func (s *WorkspaceSearchSettingsService) Update(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole, input UpdateWorkspaceSearchSettingsInput) (*dto.WorkspaceSearchSettings, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	if !actorRole.AtLeast(value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	settings := &model.WorkspaceSearchSettings{WorkspaceID: workspaceID, UpdatedBy: input.ActorID}
	now := time.Now().UTC()
	settings.CreatedAt, settings.UpdatedAt = now, now
	if input.RerankEnabled {
		if s.models == nil {
			return nil, fmt.Errorf("%w: 模型解析器不能为空", domainerrors.ErrValidation)
		}
		if input.ModelID == uuid.Nil {
			return nil, fmt.Errorf("%w: 启用 Rerank 必须提供 model_id", domainerrors.ErrValidation)
		}
		if err := value.ValidateRerankCandidateTopK(input.CandidateTopK); err != nil {
			return nil, err
		}
		if !input.FailureMode.IsValid() {
			return nil, fmt.Errorf("%w: Rerank failure_mode 无效", domainerrors.ErrValidation)
		}
		resolved, err := s.models.ResolveSelectableModel(ctx, workspaceID, input.ModelID, value.ModelTypeRerank)
		if err != nil {
			return nil, err
		}
		settings.Rerank, err = buildWorkspaceRerankSnapshot(resolved, input.CandidateTopK, input.FailureMode)
		if err != nil {
			return nil, err
		}
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.Upsert(ctx, settings); err != nil {
		return nil, err
	}
	return dto.WorkspaceSearchSettingsFromModel(settings), nil
}

func buildWorkspaceRerankSnapshot(resolved *model.ResolvedModel, candidateTopK int, failureMode value.RerankFailureMode) (*model.RerankSnapshot, error) {
	if resolved == nil || resolved.Model == nil || resolved.Provider == nil || resolved.Model.Type != value.ModelTypeRerank {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	maxDocuments, err := rerankIntParameter(resolved.Model.Parameters, "max_documents")
	if err != nil {
		return nil, err
	}
	if candidateTopK > maxDocuments {
		return nil, fmt.Errorf("%w: candidate_top_k %d 超过模型 max_documents %d", domainerrors.ErrValidation, candidateTopK, maxDocuments)
	}
	hash, err := rerankModelConfigHash(resolved)
	if err != nil {
		return nil, err
	}
	return &model.RerankSnapshot{ModelID: resolved.Model.ID, ProviderID: resolved.Provider.ID, ModelName: resolved.Model.ModelName, ModelConfigHash: hash, CandidateTopK: candidateTopK, FailureMode: failureMode}, nil
}
