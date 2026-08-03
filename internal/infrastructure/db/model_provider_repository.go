package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelProviderRepository 使用 GORM 持久化模型 Provider。
type ModelProviderRepository struct {
	db *gorm.DB
}

// NewModelProviderRepository 创建 Provider repository。
func NewModelProviderRepository(db *gorm.DB) *ModelProviderRepository {
	return &ModelProviderRepository{db: db}
}

func (r *ModelProviderRepository) Create(ctx context.Context, provider *model.ModelProvider) error {
	row, err := modelProviderToRow(provider)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return translateDBError(err, "创建模型 Provider 失败")
	}
	return nil
}

func (r *ModelProviderRepository) GetWorkspaceOwned(ctx context.Context, workspaceID, providerID uuid.UUID) (*model.ModelProvider, error) {
	return r.get(ctx, "id = ? AND scope = ? AND workspace_id = ?", providerID, value.ModelScopeWorkspace, workspaceID)
}

func (r *ModelProviderRepository) GetPlatform(ctx context.Context, providerID uuid.UUID) (*model.ModelProvider, error) {
	return r.get(ctx, "id = ? AND scope = ?", providerID, value.ModelScopePlatform)
}

func (r *ModelProviderRepository) GetVisible(ctx context.Context, workspaceID, providerID uuid.UUID) (*model.ModelProvider, error) {
	return r.get(ctx, "id = ? AND (scope = ? OR (scope = ? AND workspace_id = ?))",
		providerID, value.ModelScopePlatform, value.ModelScopeWorkspace, workspaceID)
}

func (r *ModelProviderRepository) getByID(ctx context.Context, providerID uuid.UUID) (*model.ModelProvider, error) {
	return r.get(ctx, "id = ?", providerID)
}

func (r *ModelProviderRepository) get(ctx context.Context, query string, args ...any) (*model.ModelProvider, error) {
	var row ModelProviderRow
	if err := r.db.WithContext(ctx).Where(query, args...).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取模型 Provider 失败: %w", err)
	}
	return modelProviderFromRow(&row)
}

func (r *ModelProviderRepository) ListVisible(ctx context.Context, workspaceID uuid.UUID) ([]*model.ModelProvider, error) {
	var rows []ModelProviderRow
	err := r.db.WithContext(ctx).
		Where("scope = ? OR (scope = ? AND workspace_id = ?)", value.ModelScopePlatform, value.ModelScopeWorkspace, workspaceID).
		Order("CASE scope WHEN 'workspace' THEN 0 ELSE 1 END").
		Order("lower(display_name) ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("列出可见模型 Provider 失败: %w", err)
	}
	return modelProvidersFromRows(rows)
}

func (r *ModelProviderRepository) ListPlatform(ctx context.Context) ([]*model.ModelProvider, error) {
	var rows []ModelProviderRow
	if err := r.db.WithContext(ctx).
		Where("scope = ?", value.ModelScopePlatform).
		Order("lower(display_name) ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出平台模型 Provider 失败: %w", err)
	}
	return modelProvidersFromRows(rows)
}

func modelProvidersFromRows(rows []ModelProviderRow) ([]*model.ModelProvider, error) {
	result := make([]*model.ModelProvider, 0, len(rows))
	for i := range rows {
		provider, err := modelProviderFromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		result = append(result, provider)
	}
	return result, nil
}

func (r *ModelProviderRepository) Update(ctx context.Context, provider *model.ModelProvider) error {
	row, err := modelProviderToRow(provider)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&ModelProviderRow{}).
		Where("id = ?", provider.ID).
		Updates(map[string]any{
			"display_name":           row.DisplayName,
			"description":            row.Description,
			"config":                 row.Config,
			"credentials_ciphertext": row.CredentialsCiphertext,
			"status":                 row.Status,
			"updated_at":             row.UpdatedAt,
		})
	if result.Error != nil {
		return translateDBError(result.Error, "更新模型 Provider 失败")
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *ModelProviderRepository) Delete(ctx context.Context, providerID uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&ModelProviderRow{}, "id = ?", providerID)
	if result.Error != nil {
		if isForeignKeyViolation(result.Error) {
			return domainerrors.ErrProviderInUse
		}
		return fmt.Errorf("删除模型 Provider 失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *ModelProviderRepository) CountModels(ctx context.Context, providerID uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&ModelRow{}).Where("provider_id = ?", providerID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计 Provider 模型失败: %w", err)
	}
	return count, nil
}
