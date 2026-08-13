package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
)

// WorkspaceReadinessRepository reads the minimal Workspace experience projection.
type WorkspaceReadinessRepository struct{ db *gorm.DB }

// NewWorkspaceReadinessRepository creates a readiness query repository.
func NewWorkspaceReadinessRepository(database *gorm.DB) *WorkspaceReadinessRepository {
	return &WorkspaceReadinessRepository{db: database}
}

// GetWorkspaceReadinessFacts reads all readiness facts in one Workspace transaction.
func (r *WorkspaceReadinessRepository) GetWorkspaceReadinessFacts(ctx context.Context, workspaceID uuid.UUID) (*service.WorkspaceReadinessFacts, error) {
	facts := &service.WorkspaceReadinessFacts{}
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Raw(`
			SELECT EXISTS (
				SELECT 1 FROM model_providers
				WHERE status = 'active' AND (scope = 'platform' OR (scope = 'workspace' AND workspace_id = ?))
			)`, workspaceID).Scan(&facts.HasActiveProvider).Error; err != nil {
			return fmt.Errorf("查询可用 Provider 失败: %w", err)
		}
		if err := tx.WithContext(ctx).Raw(`
			SELECT EXISTS (
				SELECT 1 FROM models
				JOIN model_providers ON model_providers.id = models.provider_id
				WHERE models.type = 'embedding' AND models.status = 'active'
				  AND model_providers.status = 'active'
				  AND models.dimensions IN (798, 1024, 2048, 3584)
				  AND (model_providers.scope = 'platform' OR (model_providers.scope = 'workspace' AND model_providers.workspace_id = ?))
			)`, workspaceID).Scan(&facts.HasSelectableEmbeddingModel).Error; err != nil {
			return fmt.Errorf("查询可选 Embedding 模型失败: %w", err)
		}
		if err := tx.WithContext(ctx).Model(&KnowledgeBaseRow{}).
			Where("workspace_id = ? AND deleted_at IS NULL", workspaceID).
			Count(&facts.KnowledgeBaseCount).Error; err != nil {
			return fmt.Errorf("统计知识库失败: %w", err)
		}
		var counts struct {
			Total, Ready, Processing, Failed int64
		}
		// SQLite 不支持 COUNT(*) FILTER，改用 SUM(CASE WHEN ...)，语义等价。
		docStatusSelect := `
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status IN ('ready', 'completed')) AS ready,
			COUNT(*) FILTER (WHERE status IN ('pending', 'processing', 'parsing_submitted', 'parsing', 'parsed', 'indexing', 'deleting')) AS processing,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed`
		if tx.Dialector.Name() == "sqlite" {
			docStatusSelect = `
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status IN ('ready', 'completed') THEN 1 ELSE 0 END), 0) AS ready,
			COALESCE(SUM(CASE WHEN status IN ('pending', 'processing', 'parsing_submitted', 'parsing', 'parsed', 'indexing', 'deleting') THEN 1 ELSE 0 END), 0) AS processing,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS failed`
		}
		if err := tx.WithContext(ctx).Table("documents").Select(docStatusSelect).
			Where("workspace_id = ? AND deleted_at IS NULL AND status <> 'deleted'", workspaceID).
			Scan(&counts).Error; err != nil {
			return fmt.Errorf("统计文档状态失败: %w", err)
		}
		facts.TotalDocuments, facts.ReadyDocuments = counts.Total, counts.Ready
		facts.ProcessingDocuments, facts.FailedDocuments = counts.Processing, counts.Failed
		if err := tx.WithContext(ctx).Table("knowledge_bases AS kb").
			Joins("JOIN knowledge_base_index_generations AS generation ON generation.id = kb.active_index_generation_id AND generation.workspace_id = kb.workspace_id").
			Where("kb.workspace_id = ? AND kb.deleted_at IS NULL AND generation.status = 'ready' AND generation.indexed_count > 0", workspaceID).
			Count(&facts.SearchableKnowledgeBaseCount).Error; err != nil {
			return fmt.Errorf("统计可检索知识库失败: %w", err)
		}
		return r.loadRecommendedResource(ctx, tx, workspaceID, facts)
	})
	if err != nil {
		return nil, err
	}
	return facts, nil
}

func (r *WorkspaceReadinessRepository) loadRecommendedResource(ctx context.Context, tx *gorm.DB, workspaceID uuid.UUID, facts *service.WorkspaceReadinessFacts) error {
	type recommendationRow struct {
		KnowledgeBaseID   uuid.UUID
		KnowledgeBaseName string
		DocumentID        *uuid.UUID
		DocumentName      string
	}
	readDocument := func(statuses []string) (*recommendationRow, error) {
		var row recommendationRow
		result := tx.WithContext(ctx).Table("documents AS document").Select(`
			document.knowledge_base_id, knowledge_base.name AS knowledge_base_name,
			document.id AS document_id, document.title AS document_name`).
			Joins("JOIN knowledge_bases AS knowledge_base ON knowledge_base.id = document.knowledge_base_id AND knowledge_base.workspace_id = document.workspace_id").
			Where("document.workspace_id = ? AND document.deleted_at IS NULL AND document.status IN ?", workspaceID, statuses).
			Order("document.updated_at DESC, document.id DESC").Limit(1).Scan(&row)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, nil
		}
		return &row, nil
	}
	var recommendation *recommendationRow
	var err error
	if facts.FailedDocuments > 0 {
		recommendation, err = readDocument([]string{"failed"})
	} else if facts.ProcessingDocuments > 0 && facts.ReadyDocuments == 0 {
		recommendation, err = readDocument([]string{"pending", "processing", "parsing_submitted", "parsing", "parsed", "indexing", "deleting"})
	}
	if err != nil {
		return fmt.Errorf("查询建议文档失败: %w", err)
	}
	if recommendation == nil {
		var row recommendationRow
		query := tx.WithContext(ctx).Table("knowledge_bases AS knowledge_base").Select(`
			knowledge_base.id AS knowledge_base_id, knowledge_base.name AS knowledge_base_name`).
			Where("knowledge_base.workspace_id = ? AND knowledge_base.deleted_at IS NULL", workspaceID)
		if facts.SearchableKnowledgeBaseCount > 0 {
			query = query.Joins("JOIN knowledge_base_index_generations AS generation ON generation.id = knowledge_base.active_index_generation_id AND generation.workspace_id = knowledge_base.workspace_id").
				Where("generation.status = 'ready' AND generation.indexed_count > 0")
		}
		result := query.Order("knowledge_base.updated_at DESC, knowledge_base.id DESC").Limit(1).Scan(&row)
		if result.Error != nil {
			return fmt.Errorf("查询建议知识库失败: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			recommendation = &row
		}
	}
	if recommendation != nil {
		knowledgeBaseID := recommendation.KnowledgeBaseID
		facts.RecommendedKnowledgeBaseID = &knowledgeBaseID
		facts.RecommendedKnowledgeBaseName = recommendation.KnowledgeBaseName
		facts.RecommendedDocumentID = recommendation.DocumentID
		facts.RecommendedDocumentName = recommendation.DocumentName
	}
	return nil
}
