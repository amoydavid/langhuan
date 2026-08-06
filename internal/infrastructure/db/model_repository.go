package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelRepository 使用 GORM 持久化具体模型。
type ModelRepository struct {
	db *gorm.DB
}

// NewModelRepository 创建 Model repository。
func NewModelRepository(db *gorm.DB) *ModelRepository {
	return &ModelRepository{db: db}
}

func (r *ModelRepository) Create(ctx context.Context, input *model.Model) error {
	row, err := modelToRow(input)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return translateDBError(err, "创建模型失败")
	}
	return nil
}

func (r *ModelRepository) GetWorkspaceOwned(ctx context.Context, workspaceID, modelID uuid.UUID) (*model.ResolvedModel, error) {
	return r.getResolved(ctx, modelID, "provider.scope = ? AND provider.workspace_id = ?", value.ModelScopeWorkspace, workspaceID)
}

func (r *ModelRepository) GetPlatform(ctx context.Context, modelID uuid.UUID) (*model.ResolvedModel, error) {
	return r.getResolved(ctx, modelID, "provider.scope = ?", value.ModelScopePlatform)
}

func (r *ModelRepository) GetVisible(ctx context.Context, workspaceID, modelID uuid.UUID) (*model.ResolvedModel, error) {
	return r.getResolved(ctx, modelID,
		"provider.scope = ? OR (provider.scope = ? AND provider.workspace_id = ?)",
		value.ModelScopePlatform, value.ModelScopeWorkspace, workspaceID)
}

func (r *ModelRepository) getResolved(ctx context.Context, modelID uuid.UUID, scopeQuery string, args ...any) (*model.ResolvedModel, error) {
	var row ModelRow
	queryArgs := append([]any{modelID}, args...)
	err := r.db.WithContext(ctx).
		Model(&ModelRow{}).
		Joins("JOIN model_providers AS provider ON provider.id = models.provider_id").
		Where("models.id = ? AND ("+scopeQuery+")", queryArgs...).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	item, err := modelFromRow(&row)
	if err != nil {
		return nil, err
	}
	provider, err := NewModelProviderRepository(r.db).getByID(ctx, row.ProviderID)
	if err != nil {
		return nil, err
	}
	return &model.ResolvedModel{Model: item, Provider: provider}, nil
}

func (r *ModelRepository) ListByProviderVisible(ctx context.Context, workspaceID, providerID uuid.UUID) ([]*model.Model, error) {
	if _, err := NewModelProviderRepository(r.db).GetVisible(ctx, workspaceID, providerID); err != nil {
		return nil, err
	}
	return r.listByProvider(ctx, providerID)
}

func (r *ModelRepository) ListByProviderPlatform(ctx context.Context, providerID uuid.UUID) ([]*model.Model, error) {
	if _, err := NewModelProviderRepository(r.db).GetPlatform(ctx, providerID); err != nil {
		return nil, err
	}
	return r.listByProvider(ctx, providerID)
}

func (r *ModelRepository) listByProvider(ctx context.Context, providerID uuid.UUID) ([]*model.Model, error) {
	var rows []ModelRow
	if err := r.db.WithContext(ctx).
		Where("provider_id = ?", providerID).
		Order("lower(display_name) ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出 Provider 模型失败: %w", err)
	}
	return modelsFromRows(rows)
}

func (r *ModelRepository) ListVisible(ctx context.Context, workspaceID uuid.UUID, modelType value.ModelType, activeOnly bool) ([]*model.ResolvedModel, error) {
	var rows []ModelRow
	query := r.db.WithContext(ctx).
		Model(&ModelRow{}).
		Joins("JOIN model_providers AS provider ON provider.id = models.provider_id").
		Where("models.type = ?", modelType).
		Where("provider.scope = ? OR (provider.scope = ? AND provider.workspace_id = ?)",
			value.ModelScopePlatform, value.ModelScopeWorkspace, workspaceID)
	if activeOnly {
		query = query.Where("models.status = ? AND provider.status = ?", value.ModelStatusActive, value.ModelStatusActive)
	}
	if err := query.
		Order("CASE provider.scope WHEN 'workspace' THEN 0 ELSE 1 END").
		Order("lower(models.display_name) ASC, models.id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出可见模型失败: %w", err)
	}

	providers := make(map[uuid.UUID]*model.ModelProvider)
	result := make([]*model.ResolvedModel, 0, len(rows))
	providerRepo := NewModelProviderRepository(r.db)
	for i := range rows {
		item, err := modelFromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		provider := providers[item.ProviderID]
		if provider == nil {
			provider, err = providerRepo.getByID(ctx, item.ProviderID)
			if err != nil {
				return nil, err
			}
			providers[item.ProviderID] = provider
		}
		result = append(result, &model.ResolvedModel{Model: item, Provider: provider})
	}
	return result, nil
}

// ListManagedVisible 返回 Workspace 可见的管理目录模型。
func (r *ModelRepository) ListManagedVisible(ctx context.Context, workspaceID uuid.UUID, filter appservice.ModelListFilter) ([]*model.ResolvedModel, error) {
	query := r.db.WithContext(ctx).
		Model(&ModelRow{}).
		Joins("JOIN model_providers AS provider ON provider.id = models.provider_id").
		Where("provider.scope = ? OR (provider.scope = ? AND provider.workspace_id = ?)",
			value.ModelScopePlatform, value.ModelScopeWorkspace, workspaceID)
	return r.listManaged(ctx, query, filter)
}

// ListManagedPlatform 返回平台连接下的管理目录模型。
func (r *ModelRepository) ListManagedPlatform(ctx context.Context, filter appservice.ModelListFilter) ([]*model.ResolvedModel, error) {
	query := r.db.WithContext(ctx).
		Model(&ModelRow{}).
		Joins("JOIN model_providers AS provider ON provider.id = models.provider_id").
		Where("provider.scope = ?", value.ModelScopePlatform)
	return r.listManaged(ctx, query, filter)
}

func (r *ModelRepository) listManaged(ctx context.Context, query *gorm.DB, filter appservice.ModelListFilter) ([]*model.ResolvedModel, error) {
	if filter.Type != nil {
		query = query.Where("models.type = ?", *filter.Type)
	}
	if filter.Status != nil {
		query = query.Where("models.status = ?", *filter.Status)
	}
	if filter.Scope != nil {
		query = query.Where("provider.scope = ?", *filter.Scope)
	}
	if filter.ProviderID != nil {
		query = query.Where("models.provider_id = ?", *filter.ProviderID)
	}
	if search := strings.TrimSpace(filter.Query); search != "" {
		pattern := "%" + search + "%"
		query = query.Where(`models.name ILIKE ? OR models.display_name ILIKE ? OR models.model_name ILIKE ? OR provider.display_name ILIKE ?`,
			pattern, pattern, pattern, pattern)
	}
	var rows []ModelRow
	if err := query.
		Order("CASE provider.scope WHEN 'workspace' THEN 0 ELSE 1 END").
		Order("lower(models.display_name) ASC, models.id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出管理模型目录失败: %w", err)
	}
	return r.resolveRows(ctx, rows)
}

func (r *ModelRepository) resolveRows(ctx context.Context, rows []ModelRow) ([]*model.ResolvedModel, error) {
	providerIDs := make([]uuid.UUID, 0, len(rows))
	seen := make(map[uuid.UUID]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ProviderID]; !ok {
			seen[row.ProviderID] = struct{}{}
			providerIDs = append(providerIDs, row.ProviderID)
		}
	}
	providerRows := make([]ModelProviderRow, 0, len(providerIDs))
	if len(providerIDs) > 0 {
		if err := r.db.WithContext(ctx).Where("id IN ?", providerIDs).Find(&providerRows).Error; err != nil {
			return nil, fmt.Errorf("批量读取模型 Provider 失败: %w", err)
		}
	}
	providers := make(map[uuid.UUID]*model.ModelProvider, len(providerRows))
	for i := range providerRows {
		provider, err := modelProviderFromRow(&providerRows[i])
		if err != nil {
			return nil, err
		}
		providers[provider.ID] = provider
	}
	result := make([]*model.ResolvedModel, 0, len(rows))
	for i := range rows {
		item, err := modelFromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		provider := providers[item.ProviderID]
		if provider == nil {
			return nil, fmt.Errorf("读取模型 %s 的 Provider 失败: %w", item.ID, domainerrors.ErrNotFound)
		}
		result = append(result, &model.ResolvedModel{Model: item, Provider: provider})
	}
	return result, nil
}

func modelsFromRows(rows []ModelRow) ([]*model.Model, error) {
	result := make([]*model.Model, 0, len(rows))
	for i := range rows {
		item, err := modelFromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *ModelRepository) Update(ctx context.Context, input *model.Model) error {
	row, err := modelToRow(input)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current ModelRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", input.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRepositoryNotFound
			}
			return fmt.Errorf("锁定模型失败: %w", err)
		}
		semanticChanged := current.ModelName != row.ModelName || !equalOptionalInt(current.Dimensions, row.Dimensions)
		if semanticChanged {
			count, err := NewModelRepository(tx).CountGenerationReferences(ctx, input.ID)
			if err != nil {
				return err
			}
			if count > 0 {
				return domainerrors.ErrImmutableModelField
			}
		}
		result := tx.WithContext(ctx).Model(&ModelRow{}).
			Where("id = ?", input.ID).
			Updates(map[string]any{
				"display_name": row.DisplayName,
				"description":  row.Description,
				"model_name":   row.ModelName,
				"dimensions":   row.Dimensions,
				"parameters":   row.Parameters,
				"status":       row.Status,
				"updated_at":   row.UpdatedAt,
			})
		if result.Error != nil {
			return translateDBError(result.Error, "更新模型失败")
		}
		if result.RowsAffected == 0 {
			return ErrRepositoryNotFound
		}
		return nil
	})
}

func equalOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (r *ModelRepository) Delete(ctx context.Context, modelID uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&ModelRow{}, "id = ?", modelID)
	if result.Error != nil {
		if isForeignKeyViolation(result.Error) {
			return domainerrors.ErrModelInUse
		}
		return fmt.Errorf("删除模型失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *ModelRepository) CountGenerationReferences(ctx context.Context, modelID uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where("embedding_model_id = ? OR rerank_model_id = ?", modelID, modelID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计模型知识库引用失败: %w", err)
	}
	return count, nil
}
