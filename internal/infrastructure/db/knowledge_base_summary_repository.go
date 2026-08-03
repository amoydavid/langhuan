package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSummaryRepository reads safe Workspace-scoped workbench projections.
type KnowledgeBaseSummaryRepository struct{ db *gorm.DB }

// NewKnowledgeBaseSummaryRepository creates a KnowledgeBase summary query repository.
func NewKnowledgeBaseSummaryRepository(database *gorm.DB) *KnowledgeBaseSummaryRepository {
	return &KnowledgeBaseSummaryRepository{db: database}
}

// GetKnowledgeBaseSummaryFacts reads all summary facts in one Workspace transaction.
func (r *KnowledgeBaseSummaryRepository) GetKnowledgeBaseSummaryFacts(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) (*service.KnowledgeBaseSummaryFacts, error) {
	facts := &service.KnowledgeBaseSummaryFacts{}
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var knowledgeBase KnowledgeBaseRow
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, knowledgeBaseID).
			First(&knowledgeBase).Error; err != nil {
			return translateDBError(err, "读取知识库摘要失败")
		}
		facts.KnowledgeBaseID = knowledgeBase.ID
		facts.KnowledgeBaseName = knowledgeBase.Name
		facts.ContentVersion = knowledgeBase.ContentVersion

		var counts struct {
			Total, File, FAQ, Web, Ready, Processing, Failed int64
		}
		if err := tx.WithContext(ctx).Table("documents").Select(`
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE kind = 'file') AS file,
			COUNT(*) FILTER (WHERE kind = 'faq') AS faq,
			COUNT(*) FILTER (WHERE kind = 'web') AS web,
			COUNT(*) FILTER (WHERE status IN ('ready', 'completed')) AS ready,
			COUNT(*) FILTER (WHERE status IN ('pending', 'processing', 'parsing_submitted', 'parsing', 'parsed', 'indexing', 'deleting')) AS processing,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed`).
			Where("workspace_id = ? AND knowledge_base_id = ? AND deleted_at IS NULL AND status <> 'deleted'", workspaceID, knowledgeBaseID).
			Scan(&counts).Error; err != nil {
			return fmt.Errorf("统计知识库文档失败: %w", err)
		}
		facts.TotalDocuments, facts.FileDocuments, facts.FAQDocuments, facts.WebDocuments = counts.Total, counts.File, counts.FAQ, counts.Web
		facts.ReadyDocuments, facts.ProcessingDocuments, facts.FailedDocuments = counts.Ready, counts.Processing, counts.Failed

		if knowledgeBase.ActiveIndexGenerationID != nil {
			active, err := r.getGenerationFacts(ctx, tx, workspaceID, knowledgeBaseID, *knowledgeBase.ActiveIndexGenerationID)
			if err != nil {
				return err
			}
			facts.ActiveGeneration = active
		}
		candidate, err := r.getCandidateGenerationFacts(ctx, tx, workspaceID, knowledgeBaseID, knowledgeBase.ActiveIndexGenerationID)
		if err != nil {
			return err
		}
		facts.CandidateGeneration = candidate

		var hasRunningJob bool
		if err := tx.WithContext(ctx).Raw(`SELECT EXISTS (
			SELECT 1 FROM jobs
			WHERE workspace_id = ? AND knowledge_base_id = ? AND status IN ('pending', 'queued', 'running')
		)`, workspaceID, knowledgeBaseID).Scan(&hasRunningJob).Error; err != nil {
			return fmt.Errorf("查询知识库运行任务失败: %w", err)
		}
		facts.HasUpdatingWork = counts.Processing > 0 || hasRunningJob || (candidate != nil && candidate.Status == value.IndexGenerationBuilding)

		recent, err := r.listJobFacts(ctx, tx, workspaceID, knowledgeBaseID, service.KnowledgeBaseJobFactsFilter{Limit: 5})
		if err != nil {
			return err
		}
		facts.RecentJobs = recent
		blockers, err := r.listBlockerFacts(ctx, tx, workspaceID, knowledgeBaseID, facts.ActiveGeneration, candidate)
		if err != nil {
			return err
		}
		facts.Blockers = blockers
		return nil
	})
	if err != nil {
		return nil, err
	}
	return facts, nil
}

// ListKnowledgeBaseJobFacts returns one stable seek page without loading payload or external IDs.
func (r *KnowledgeBaseSummaryRepository) ListKnowledgeBaseJobFacts(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID, filter service.KnowledgeBaseJobFactsFilter) ([]service.KnowledgeBaseJobFacts, error) {
	var facts []service.KnowledgeBaseJobFacts
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var knowledgeBase KnowledgeBaseRow
		if err := tx.WithContext(ctx).Select("id").
			Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, knowledgeBaseID).
			First(&knowledgeBase).Error; err != nil {
			return translateDBError(err, "读取知识库任务列表失败")
		}
		items, err := r.listJobFacts(ctx, tx, workspaceID, knowledgeBaseID, filter)
		if err != nil {
			return err
		}
		facts = items
		return nil
	})
	if err != nil {
		return nil, err
	}
	return facts, nil
}

type knowledgeBaseGenerationFactsRow struct {
	ID                    uuid.UUID
	EmbeddingModelID      uuid.UUID
	Status                string
	ModelName             string
	ModelDisplayName      string
	EmbeddingDimension    int
	ChunkerVersion        int
	ChunkingConfig        JSONMap
	RetrievalConfig       JSONMap
	SourceContentVersion  int64
	IndexedContentVersion int64
	DocumentCount         int64
	ChunkCount            int64
	IndexedCount          int64
	ManualEditCount       int64
	DisabledChunkCount    int64
	ErrorClass            string
	ErrorMessage          string
	CreatedAt             time.Time
	ReadyAt               *time.Time
	ActivatedAt           *time.Time
}

func (r *KnowledgeBaseSummaryRepository) generationFactsQuery(tx *gorm.DB, workspaceID, knowledgeBaseID uuid.UUID) *gorm.DB {
	return tx.Table("knowledge_base_index_generations AS generation").Select(`
		generation.id, generation.embedding_model_id, generation.status, generation.model_name,
		COALESCE(NULLIF(models.display_name, ''), NULLIF(generation.model_name, ''), '未命名模型') AS model_display_name,
		generation.embedding_dimension, generation.chunker_version, generation.chunking_config,
		generation.retrieval_config, generation.source_content_version, generation.indexed_content_version,
		generation.document_count, generation.chunk_count, generation.indexed_count,
		generation.manual_edit_count, generation.disabled_chunk_count, generation.error_class,
		generation.error_message, generation.created_at, generation.ready_at, generation.activated_at`).
		Joins("LEFT JOIN models ON models.id = generation.embedding_model_id").
		Where("generation.workspace_id = ? AND generation.knowledge_base_id = ?", workspaceID, knowledgeBaseID)
}

func (r *KnowledgeBaseSummaryRepository) getGenerationFacts(ctx context.Context, tx *gorm.DB, workspaceID, knowledgeBaseID, generationID uuid.UUID) (*service.KnowledgeBaseGenerationFacts, error) {
	var row knowledgeBaseGenerationFactsRow
	if err := r.generationFactsQuery(tx, workspaceID, knowledgeBaseID).WithContext(ctx).
		Where("generation.id = ?", generationID).First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取生效索引版本失败")
	}
	return generationFactsFromRow(row), nil
}

func (r *KnowledgeBaseSummaryRepository) getCandidateGenerationFacts(ctx context.Context, tx *gorm.DB, workspaceID, knowledgeBaseID uuid.UUID, activeID *uuid.UUID) (*service.KnowledgeBaseGenerationFacts, error) {
	var row knowledgeBaseGenerationFactsRow
	query := r.generationFactsQuery(tx, workspaceID, knowledgeBaseID).WithContext(ctx).
		Where("generation.status <> ?", string(value.IndexGenerationRetired))
	if activeID != nil {
		query = query.Where("generation.id <> ?", *activeID)
	}
	result := query.Order("generation.created_at DESC, generation.id DESC").Limit(1).Scan(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("读取候选索引版本失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return generationFactsFromRow(row), nil
}

func generationFactsFromRow(row knowledgeBaseGenerationFactsRow) *service.KnowledgeBaseGenerationFacts {
	return &service.KnowledgeBaseGenerationFacts{
		ID: row.ID, EmbeddingModelID: row.EmbeddingModelID, Status: value.IndexGenerationStatus(row.Status),
		ModelName: row.ModelName, ModelDisplayName: row.ModelDisplayName, EmbeddingDimension: row.EmbeddingDimension,
		ChunkerVersion: row.ChunkerVersion, ChunkingConfig: cloneSummaryJSON(row.ChunkingConfig), RetrievalConfig: cloneSummaryJSON(row.RetrievalConfig),
		SourceContentVersion: row.SourceContentVersion, IndexedContentVersion: row.IndexedContentVersion,
		DocumentCount: row.DocumentCount, ChunkCount: row.ChunkCount, IndexedCount: row.IndexedCount,
		ManualEditCount: row.ManualEditCount, DisabledChunkCount: row.DisabledChunkCount,
		ErrorClass: row.ErrorClass, ErrorMessage: row.ErrorMessage, CreatedAt: row.CreatedAt,
		ReadyAt: row.ReadyAt, ActivatedAt: row.ActivatedAt,
	}
}

type knowledgeBaseJobFactsRow struct {
	ID                uuid.UUID
	DocumentID        *uuid.UUID
	IndexGenerationID *uuid.UUID
	Type              string
	Status            string
	TargetType        string
	TargetDisplayName string
	TargetCreatedAt   *time.Time
	TargetModelName   string
	Attempts          int
	ErrorClass        string
	ErrorMessage      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (r *KnowledgeBaseSummaryRepository) listJobFacts(ctx context.Context, tx *gorm.DB, workspaceID, knowledgeBaseID uuid.UUID, filter service.KnowledgeBaseJobFactsFilter) ([]service.KnowledgeBaseJobFacts, error) {
	query := tx.WithContext(ctx).Table("jobs AS job").Select(`
		job.id, job.document_id, job.index_generation_id, job.type, job.status, job.attempts,
		job.error_class, job.error_message, job.created_at, job.updated_at,
		CASE WHEN job.document_id IS NOT NULL THEN 'document' ELSE 'generation' END AS target_type,
		CASE WHEN job.document_id IS NOT NULL
			THEN COALESCE(NULLIF(file_node.name, ''), NULLIF(document.title, ''), '未命名文档')
			ELSE '' END AS target_display_name,
		generation.created_at AS target_created_at,
		COALESCE(NULLIF(models.display_name, ''), NULLIF(generation.model_name, ''), '') AS target_model_name`).
		Joins("LEFT JOIN documents AS document ON document.workspace_id = job.workspace_id AND document.knowledge_base_id = job.knowledge_base_id AND document.id = job.document_id").
		Joins("LEFT JOIN file_tree_nodes AS file_node ON file_node.workspace_id = document.workspace_id AND file_node.knowledge_base_id = document.knowledge_base_id AND file_node.document_id = document.id AND file_node.node_type = 'file'").
		Joins("LEFT JOIN knowledge_base_index_generations AS generation ON generation.workspace_id = job.workspace_id AND generation.knowledge_base_id = job.knowledge_base_id AND generation.id = job.index_generation_id").
		Joins("LEFT JOIN models ON models.id = generation.embedding_model_id").
		Where("job.workspace_id = ? AND job.knowledge_base_id = ?", workspaceID, knowledgeBaseID)
	if filter.DocumentID != nil {
		query = query.Where("job.document_id = ?", *filter.DocumentID)
	}
	if filter.Status != "" {
		query = query.Where("job.status = ?", string(filter.Status))
	}
	if filter.BeforeCreatedAt != nil && filter.BeforeID != nil {
		query = query.Where("(job.created_at < ? OR (job.created_at = ? AND job.id < ?))", *filter.BeforeCreatedAt, *filter.BeforeCreatedAt, *filter.BeforeID)
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	var rows []knowledgeBaseJobFactsRow
	if err := query.Order("job.created_at DESC, job.id DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出知识库任务失败: %w", err)
	}
	result := make([]service.KnowledgeBaseJobFacts, 0, len(rows))
	for _, row := range rows {
		result = append(result, service.KnowledgeBaseJobFacts{
			ID: row.ID, DocumentID: row.DocumentID, IndexGenerationID: row.IndexGenerationID,
			Type: row.Type, Status: value.JobStatus(row.Status), TargetType: row.TargetType,
			TargetDisplayName: row.TargetDisplayName, TargetCreatedAt: row.TargetCreatedAt, TargetModelName: row.TargetModelName,
			Attempts: row.Attempts, ErrorClass: row.ErrorClass, ErrorMessage: row.ErrorMessage,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return result, nil
}

type blockerDocumentRow struct {
	ID          uuid.UUID
	DisplayName string
}

func (r *KnowledgeBaseSummaryRepository) listBlockerFacts(ctx context.Context, tx *gorm.DB, workspaceID, knowledgeBaseID uuid.UUID, active, candidate *service.KnowledgeBaseGenerationFacts) ([]service.KnowledgeBaseBlockerFacts, error) {
	var documents []blockerDocumentRow
	if err := tx.WithContext(ctx).Table("documents AS document").Select(`
		document.id, COALESCE(NULLIF(file_node.name, ''), NULLIF(document.title, ''), '未命名文档') AS display_name`).
		Joins("LEFT JOIN file_tree_nodes AS file_node ON file_node.workspace_id = document.workspace_id AND file_node.knowledge_base_id = document.knowledge_base_id AND file_node.document_id = document.id AND file_node.node_type = 'file'").
		Where("document.workspace_id = ? AND document.knowledge_base_id = ? AND document.deleted_at IS NULL AND document.status = 'failed'", workspaceID, knowledgeBaseID).
		Order("document.updated_at DESC, document.id DESC").Limit(20).Scan(&documents).Error; err != nil {
		return nil, fmt.Errorf("列出失败文档阻断项失败: %w", err)
	}
	result := make([]service.KnowledgeBaseBlockerFacts, 0, len(documents)+2)
	for _, document := range documents {
		result = append(result, service.KnowledgeBaseBlockerFacts{
			Code: "document_processing_failed", ResourceType: "document", ResourceID: document.ID,
			ResourceDisplayName: document.DisplayName,
		})
	}
	if candidate != nil && (candidate.Status == value.IndexGenerationFailed || candidate.Status == value.IndexGenerationStale) {
		code := "generation_build_failed"
		if candidate.Status == value.IndexGenerationStale {
			code = "generation_stale"
		}
		result = append(result, service.KnowledgeBaseBlockerFacts{
			Code: code, ResourceType: "generation", ResourceID: candidate.ID, ResourceDisplayName: candidate.ModelDisplayName,
		})
	}
	if active != nil {
		var state struct {
			ModelStatus, ProviderStatus, ModelDisplayName string
		}
		query := tx.WithContext(ctx).Table("models").Select(`
			models.status AS model_status, model_providers.status AS provider_status,
			COALESCE(NULLIF(models.display_name, ''), NULLIF(models.model_name, ''), '') AS model_display_name`).
			Joins("JOIN model_providers ON model_providers.id = models.provider_id").
			Where("models.id = ?", active.EmbeddingModelID).Limit(1).Scan(&state)
		if query.Error != nil {
			return nil, fmt.Errorf("读取生效索引模型状态失败: %w", query.Error)
		}
		if query.RowsAffected == 0 || strings.TrimSpace(state.ModelStatus) != string(value.ModelStatusActive) || strings.TrimSpace(state.ProviderStatus) != string(value.ModelStatusActive) {
			name := state.ModelDisplayName
			if strings.TrimSpace(name) == "" {
				name = active.ModelDisplayName
			}
			result = append(result, service.KnowledgeBaseBlockerFacts{
				Code: "active_model_unavailable", ResourceType: "model", ResourceID: active.EmbeddingModelID,
				ResourceDisplayName: name,
			})
		}
	}
	return result, nil
}

func cloneSummaryJSON(input JSONMap) map[string]any {
	result := make(map[string]any, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}
