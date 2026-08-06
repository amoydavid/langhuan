package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseRepository persists KnowledgeBases and their model references.
type KnowledgeBaseRepository struct {
	db *gorm.DB
}

// NewKnowledgeBaseRepository creates a KnowledgeBase repository.
func NewKnowledgeBaseRepository(db *gorm.DB) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{db: db}
}

// Create validates model selectability and inserts the KnowledgeBase atomically.
func (r *KnowledgeBaseRepository) Create(ctx context.Context, kb *model.KnowledgeBase) (*model.ResolvedModel, error) {
	var resolved *model.ResolvedModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, provider, err := r.lockSelectableModel(ctx, tx, kb.WorkspaceID, kb.EmbeddingModelID, value.ModelTypeEmbedding)
		if err != nil {
			return err
		}
		row, err := knowledgeBaseToRow(kb)
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return translateDBError(err, "创建知识库失败")
		}
		resolved, err = resolvedModelFromRows(item, provider)
		return err
	})
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// ResolveSelectable resolves an active visible Embedding model for a Workspace.
func (r *KnowledgeBaseRepository) ResolveSelectable(ctx context.Context, workspaceID, modelID uuid.UUID) (*model.ResolvedModel, error) {
	item, provider, err := r.lockSelectableModel(ctx, r.db, workspaceID, modelID, value.ModelTypeEmbedding)
	if err != nil {
		return nil, err
	}
	return resolvedModelFromRows(item, provider)
}

// ResolveSelectableModel 解析当前 Workspace 可见、Provider/Model active、
// 指定类型的可选模型快照，使用 FOR SHARE 锁。
func (r *KnowledgeBaseRepository) ResolveSelectableModel(ctx context.Context, workspaceID, modelID uuid.UUID, modelType value.ModelType) (*model.ResolvedModel, error) {
	item, provider, err := r.lockSelectableModel(ctx, r.db, workspaceID, modelID, modelType)
	if err != nil {
		return nil, err
	}
	return resolvedModelFromRows(item, provider)
}

// Get reads one KnowledgeBase for authorization and document ownership checks.
func (r *KnowledgeBaseRepository) Get(ctx context.Context, workspaceID, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := r.db.WithContext(ctx).First(&row, "id = ? AND workspace_id = ?", id, workspaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取知识库失败: %w", err)
	}
	return knowledgeBaseFromRow(&row)
}

// List reads KnowledgeBases without loading model details.
func (r *KnowledgeBaseRepository) List(ctx context.Context, workspaceID uuid.UUID) ([]*model.KnowledgeBase, error) {
	var rows []KnowledgeBaseRow
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出知识库失败: %w", err)
	}
	result := make([]*model.KnowledgeBase, 0, len(rows))
	for index := range rows {
		kb, err := knowledgeBaseFromRow(&rows[index])
		if err != nil {
			return nil, err
		}
		result = append(result, kb)
	}
	return result, nil
}

// GetResolved reads a KnowledgeBase and its bound model even when disabled.
func (r *KnowledgeBaseRepository) GetResolved(ctx context.Context, workspaceID, id uuid.UUID) (*model.ResolvedKnowledgeBase, error) {
	var row knowledgeBaseResolvedRow
	query := r.resolvedQuery().Where("knowledge_bases.id = ? AND knowledge_bases.workspace_id = ?", id, workspaceID)
	if err := query.WithContext(ctx).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取知识库模型失败: %w", err)
	}
	return knowledgeBaseResolvedFromRow(&row)
}

// ListResolved lists KnowledgeBases and bound models, including disabled bindings.
func (r *KnowledgeBaseRepository) ListResolved(ctx context.Context, workspaceID uuid.UUID, allowedIDs []uuid.UUID) ([]*model.ResolvedKnowledgeBase, error) {
	var rows []knowledgeBaseResolvedRow
	query := r.resolvedQuery().WithContext(ctx).Where("knowledge_bases.workspace_id = ?", workspaceID)
	if allowedIDs != nil {
		query = query.Where("knowledge_bases.id IN ?", allowedIDs)
	}
	if err := query.
		Order("knowledge_bases.created_at DESC, knowledge_bases.id DESC").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出知识库模型失败: %w", err)
	}
	result := make([]*model.ResolvedKnowledgeBase, 0, len(rows))
	for index := range rows {
		resolved, err := knowledgeBaseResolvedFromRow(&rows[index])
		if err != nil {
			return nil, err
		}
		result = append(result, resolved)
	}
	return result, nil
}

// UpdateBasics updates only name and/or description inside Workspace scope.
func (r *KnowledgeBaseRepository) UpdateBasics(ctx context.Context, input appservice.UpdateKnowledgeBaseBasicsInput) error {
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.Description != nil {
		updates["description"] = *input.Description
	}
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, input.WorkspaceID, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&KnowledgeBaseRow{}).
			Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", input.WorkspaceID, input.KnowledgeBaseID).
			Updates(updates)
		if result.Error != nil {
			return translateDBError(result.Error, "更新知识库基本信息失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// ListDueFeishuKBs 返回所有飞书来源（feishu_drive/feishu_wiki）且 source_config.next_sync_at <= now
// 的知识库。connectionID 非零值时仅返回绑定该 connection 的 KB。next_sync_at 字段缺失的 KB 视为
// 立即到期（首次同步）。结果按 workspace_id 排序以保证 Tick 分组稳定。
func (r *KnowledgeBaseRepository) ListDueFeishuKBs(ctx context.Context, now time.Time, connectionID uuid.UUID) ([]appservice.DueKnowledgeBase, error) {
	type dueRow struct {
		WorkspaceID        uuid.UUID `gorm:"column:workspace_id"`
		ID                 uuid.UUID `gorm:"column:id"`
		SourceConnectionID uuid.UUID `gorm:"column:source_connection_id"`
	}
	var rows []dueRow
	query := r.db.WithContext(ctx).Table("knowledge_bases").
		Select("workspace_id, id, source_connection_id").
		Where("deleted_at IS NULL").
		Where("source_type IN ?", []string{string(value.SourceTypeFeishuDrive), string(value.SourceTypeFeishuWiki)}).
		Where("source_connection_id IS NOT NULL").
		Where("(source_config->>'next_sync_at')::timestamptz <= ?", now.UTC())
	if connectionID != uuid.Nil {
		query = query.Where("source_connection_id = ?", connectionID)
	}
	if err := query.Order("workspace_id, id").Scan(&rows).Error; err != nil {
		return nil, translateDBError(err, "列出到期飞书知识库失败")
	}
	result := make([]appservice.DueKnowledgeBase, 0, len(rows))
	for _, row := range rows {
		result = append(result, appservice.DueKnowledgeBase{
			WorkspaceID: row.WorkspaceID, ID: row.ID, SourceConnectionID: row.SourceConnectionID,
		})
	}
	return result, nil
}

// UpdateNextSyncAt 更新某个知识库 source_config.next_sync_at 字段。
// nextSyncAt 为零值时从 source_config 中删除 next_sync_at 字段（停止定时同步）。
func (r *KnowledgeBaseRepository) UpdateNextSyncAt(ctx context.Context, workspaceID, kbID uuid.UUID, nextSyncAt time.Time) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var (
			execSQL string
			args    []any
		)
		now := time.Now().UTC()
		if nextSyncAt.IsZero() {
			execSQL = "UPDATE knowledge_bases SET source_config = source_config - 'next_sync_at', updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{now, workspaceID, kbID}
		} else {
			execSQL = "UPDATE knowledge_bases SET source_config = jsonb_set(source_config, '{next_sync_at}', to_jsonb(?::timestamptz)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{nextSyncAt.UTC(), now, workspaceID, kbID}
		}
		result := tx.WithContext(ctx).Exec(execSQL, args...)
		if result.Error != nil {
			return translateDBError(result.Error, "更新知识库 next_sync_at 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// UpdateSyncCursor 写回增量同步游标 source_config.sync_cursor（RFC3339）。
// 参照 UpdateNextSyncAt 的 jsonb_set 模式。cursor 为零值时清除字段。
func (r *KnowledgeBaseRepository) UpdateSyncCursor(ctx context.Context, workspaceID, kbID uuid.UUID, cursor time.Time) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var (
			execSQL string
			args    []any
		)
		now := time.Now().UTC()
		if cursor.IsZero() {
			execSQL = "UPDATE knowledge_bases SET source_config = source_config - 'sync_cursor', updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{now, workspaceID, kbID}
		} else {
			execSQL = "UPDATE knowledge_bases SET source_config = jsonb_set(source_config, '{sync_cursor}', to_jsonb(?::timestamptz)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{cursor.UTC(), now, workspaceID, kbID}
		}
		result := tx.WithContext(ctx).Exec(execSQL, args...)
		if result.Error != nil {
			return translateDBError(result.Error, "更新知识库 sync_cursor 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

func (r *KnowledgeBaseRepository) lockSelectableModel(ctx context.Context, tx *gorm.DB, workspaceID, modelID uuid.UUID, modelType value.ModelType) (*ModelRow, *ModelProviderRow, error) {
	var item ModelRow
	err := tx.WithContext(ctx).Table("models").Select("models.*").
		Joins("JOIN model_providers ON model_providers.id = models.provider_id").
		Where("models.id = ? AND (model_providers.scope = 'platform' OR (model_providers.scope = 'workspace' AND model_providers.workspace_id = ?))", modelID, workspaceID).
		Clauses(clause.Locking{Strength: "SHARE"}).First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, domainerrors.ErrModelNotVisible
		}
		return nil, nil, fmt.Errorf("锁定可选模型失败: %w", err)
	}
	var provider ModelProviderRow
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&provider, "id = ?", item.ProviderID).Error; err != nil {
		return nil, nil, fmt.Errorf("锁定模型 Provider 失败: %w", err)
	}
	if value.ModelType(item.Type) != modelType {
		return nil, nil, domainerrors.ErrUnsupportedModelType
	}
	if value.ModelStatus(provider.Status) != value.ModelStatusActive {
		return nil, nil, domainerrors.ErrProviderDisabled
	}
	if value.ModelStatus(item.Status) != value.ModelStatusActive {
		return nil, nil, domainerrors.ErrModelDisabled
	}
	if modelType == value.ModelTypeEmbedding {
		if item.Dimensions == nil || !value.IsSupportedEmbeddingDimension(*item.Dimensions) {
			return nil, nil, domainerrors.ErrUnsupportedEmbeddingDimension
		}
	}
	return &item, &provider, nil
}

func knowledgeBaseToRow(kb *model.KnowledgeBase) (*KnowledgeBaseRow, error) {
	return knowledgeBaseV2ToRow(kb), nil
}

func knowledgeBaseFromRow(row *KnowledgeBaseRow) (*model.KnowledgeBase, error) {
	return knowledgeBaseV2FromRow(row), nil
}

type knowledgeBaseResolvedRow struct {
	KnowledgeBaseRow
	GenerationChunkingConfig  JSONMap    `gorm:"column:resolved_generation_chunking_config"`
	GenerationRetrievalConfig JSONMap    `gorm:"column:resolved_generation_retrieval_config"`
	ModelID                   uuid.UUID  `gorm:"column:resolved_model_id"`
	ModelNameKey              string     `gorm:"column:resolved_model_name_key"`
	ModelDisplayName          string     `gorm:"column:resolved_model_display_name"`
	ModelDescription          string     `gorm:"column:resolved_model_description"`
	ModelType                 string     `gorm:"column:resolved_model_type"`
	UpstreamModel             string     `gorm:"column:resolved_upstream_model"`
	ModelDimensions           *int       `gorm:"column:resolved_model_dimensions"`
	ModelParameters           JSONMap    `gorm:"column:resolved_model_parameters"`
	ModelStatus               string     `gorm:"column:resolved_model_status"`
	ModelCreatedBy            *uuid.UUID `gorm:"column:resolved_model_created_by"`
	ModelCreatedAt            time.Time  `gorm:"column:resolved_model_created_at"`
	ModelUpdatedAt            time.Time  `gorm:"column:resolved_model_updated_at"`
	ProviderID                uuid.UUID  `gorm:"column:resolved_provider_id"`
	ProviderScope             string     `gorm:"column:resolved_provider_scope"`
	ProviderWorkspaceID       *uuid.UUID `gorm:"column:resolved_provider_workspace_id"`
	ProviderName              string     `gorm:"column:resolved_provider_name"`
	ProviderDisplayName       string     `gorm:"column:resolved_provider_display_name"`
	ProviderDescription       string     `gorm:"column:resolved_provider_description"`
	ProviderKey               string     `gorm:"column:resolved_provider_key"`
	ProviderConfig            JSONMap    `gorm:"column:resolved_provider_config"`
	ProviderStatus            string     `gorm:"column:resolved_provider_status"`
	ProviderCreatedBy         *uuid.UUID `gorm:"column:resolved_provider_created_by"`
	ProviderCreatedAt         time.Time  `gorm:"column:resolved_provider_created_at"`
	ProviderUpdatedAt         time.Time  `gorm:"column:resolved_provider_updated_at"`
}

func (r *KnowledgeBaseRepository) resolvedQuery() *gorm.DB {
	return r.db.Table("knowledge_bases").Select(`knowledge_bases.*,
		knowledge_base_index_generations.chunking_config AS resolved_generation_chunking_config,
		knowledge_base_index_generations.retrieval_config AS resolved_generation_retrieval_config,
		models.id AS resolved_model_id, models.name AS resolved_model_name_key,
		models.display_name AS resolved_model_display_name, models.description AS resolved_model_description,
		models.type AS resolved_model_type, models.model_name AS resolved_upstream_model,
		models.dimensions AS resolved_model_dimensions, models.parameters AS resolved_model_parameters,
		models.status AS resolved_model_status, models.created_by AS resolved_model_created_by,
		models.created_at AS resolved_model_created_at, models.updated_at AS resolved_model_updated_at,
		model_providers.id AS resolved_provider_id, model_providers.scope AS resolved_provider_scope,
		model_providers.workspace_id AS resolved_provider_workspace_id, model_providers.name AS resolved_provider_name,
		model_providers.display_name AS resolved_provider_display_name,
		model_providers.description AS resolved_provider_description, model_providers.provider AS resolved_provider_key,
		model_providers.config AS resolved_provider_config, model_providers.status AS resolved_provider_status,
		model_providers.created_by AS resolved_provider_created_by,
		model_providers.created_at AS resolved_provider_created_at, model_providers.updated_at AS resolved_provider_updated_at`).
		Joins("LEFT JOIN knowledge_base_index_generations ON knowledge_base_index_generations.id = knowledge_bases.active_index_generation_id AND knowledge_base_index_generations.workspace_id = knowledge_bases.workspace_id").
		Joins("LEFT JOIN models ON models.id = knowledge_base_index_generations.embedding_model_id").
		Joins("LEFT JOIN model_providers ON model_providers.id = models.provider_id")
}

func knowledgeBaseResolvedFromRow(row *knowledgeBaseResolvedRow) (*model.ResolvedKnowledgeBase, error) {
	kb, err := knowledgeBaseFromRow(&row.KnowledgeBaseRow)
	if err != nil {
		return nil, err
	}
	item := &ModelRow{
		ID: row.ModelID, ProviderID: row.ProviderID, Name: row.ModelNameKey,
		DisplayName: row.ModelDisplayName, Description: row.ModelDescription,
		Type: row.ModelType, ModelName: row.UpstreamModel, Dimensions: row.ModelDimensions,
		Parameters: row.ModelParameters, Status: row.ModelStatus, CreatedBy: row.ModelCreatedBy,
		CreatedAt: row.ModelCreatedAt, UpdatedAt: row.ModelUpdatedAt,
	}
	provider := &ModelProviderRow{
		ID: row.ProviderID, Scope: row.ProviderScope, WorkspaceID: row.ProviderWorkspaceID,
		Name: row.ProviderName, DisplayName: row.ProviderDisplayName, Description: row.ProviderDescription,
		Provider: row.ProviderKey, Config: row.ProviderConfig, Status: row.ProviderStatus,
		CreatedBy: row.ProviderCreatedBy, CreatedAt: row.ProviderCreatedAt, UpdatedAt: row.ProviderUpdatedAt,
	}
	resolved, err := resolvedModelFromRows(item, provider)
	if err != nil {
		return nil, err
	}
	kb.EmbeddingModelID = row.ModelID
	chunking := value.DefaultChunkingConfig()
	if size, ok := intFromJSON(row.GenerationChunkingConfig["chunk_size"]); ok {
		chunking.ChunkSize = size
	}
	if overlap, ok := intFromJSON(row.GenerationChunkingConfig["chunk_overlap"]); ok {
		chunking.ChunkOverlap = overlap
	}
	if strategy, ok := row.GenerationChunkingConfig["strategy"].(string); ok {
		chunking.Strategy = value.ChunkingStrategy(strategy)
	}
	if enabled, ok := row.GenerationChunkingConfig["enable_parent_child"].(bool); ok {
		chunking.EnableParentChild = enabled
	}
	if parentSize, ok := intFromJSON(row.GenerationChunkingConfig["parent_chunk_size"]); ok {
		chunking.ParentChunkSize = parentSize
	}
	if childSize, ok := intFromJSON(row.GenerationChunkingConfig["child_chunk_size"]); ok {
		chunking.ChildChunkSize = childSize
	}
	kb.ChunkingConfig = chunking.Normalize()
	return &model.ResolvedKnowledgeBase{
		KnowledgeBase: kb, EmbeddingModel: resolved,
		RetrievalConfig: normalizedDomainMap(row.GenerationRetrievalConfig),
	}, nil
}

func resolvedModelFromRows(item *ModelRow, provider *ModelProviderRow) (*model.ResolvedModel, error) {
	domainModel, err := modelFromRow(item)
	if err != nil {
		return nil, err
	}
	domainProvider, err := modelProviderFromRow(provider)
	if err != nil {
		return nil, err
	}
	return &model.ResolvedModel{Model: domainModel, Provider: domainProvider}, nil
}

func intFromJSON(input any) (int, bool) {
	switch number := input.(type) {
	case int:
		return number, true
	case int64:
		return int(number), true
	case float64:
		return int(number), true
	default:
		return 0, false
	}
}
