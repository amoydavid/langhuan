package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/dto"
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current ModelProviderRow
		if err := tx.Where("id = ?", provider.ID).First(&current).Error; err != nil {
			return translateDBError(err, "更新模型 Provider 失败")
		}
		// Provider config 属于语义字段：被 Generation 引用后不得变更，只允许轮换凭证。
		if !providerConfigEqual(current.Config, row.Config) {
			count, countErr := NewModelProviderRepository(tx).CountGenerationReferences(ctx, provider.ID)
			if countErr != nil {
				return countErr
			}
			if count > 0 {
				return domainerrors.ErrImmutableModelField
			}
		}
		result := tx.Model(&ModelProviderRow{}).
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
	})
}

// providerConfigEqual 比较两份 Provider config JSONMap 是否语义相等。
func providerConfigEqual(left, right JSONMap) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		other, ok := right[key]
		if !ok {
			return false
		}
		leftRaw, err := json.Marshal(value)
		if err != nil {
			return false
		}
		rightRaw, err := json.Marshal(other)
		if err != nil {
			return false
		}
		if !bytes.Equal(leftRaw, rightRaw) {
			return false
		}
	}
	return true
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

// CountModelsByProvider 用一次分组查询返回多条连接的模型统计。
func (r *ModelProviderRepository) CountModelsByProvider(ctx context.Context, providerIDs []uuid.UUID) (map[uuid.UUID]dto.ProviderModelCounts, error) {
	result := make(map[uuid.UUID]dto.ProviderModelCounts, len(providerIDs))
	if len(providerIDs) == 0 {
		return result, nil
	}
	type countRow struct {
		ProviderID uuid.UUID
		Total      int64
		Active     int64
		Embedding  int64
		Rerank     int64
	}
	var rows []countRow
	err := r.db.WithContext(ctx).
		Model(&ModelRow{}).
		Select(`provider_id,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = ?) AS active,
			COUNT(*) FILTER (WHERE type = ?) AS embedding,
			COUNT(*) FILTER (WHERE type = ?) AS rerank`,
			value.ModelStatusActive, value.ModelTypeEmbedding, value.ModelTypeRerank).
		Where("provider_id IN ?", providerIDs).
		Group("provider_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("统计 Provider 模型分布失败: %w", err)
	}
	for _, row := range rows {
		result[row.ProviderID] = dto.ProviderModelCounts{
			Total: row.Total, Active: row.Active, Embedding: row.Embedding, Rerank: row.Rerank,
		}
	}
	return result, nil
}

// CountGenerationReferences 统计引用该 Provider 的 Generation 数量，
// 同时覆盖 embedding provider_id 与 rerank_provider_id。
func (r *ModelProviderRepository) CountGenerationReferences(ctx context.Context, providerID uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where("provider_id = ? OR rerank_provider_id = ?", providerID, providerID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计 Provider Generation 引用失败: %w", err)
	}
	return count, nil
}
