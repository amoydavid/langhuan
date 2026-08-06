package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

const recentKnowledgeBaseJobLimit = 5

// KnowledgeBaseGenerationFacts is one immutable Generation projection without ORM concerns.
type KnowledgeBaseGenerationFacts struct {
	ID, EmbeddingModelID  uuid.UUID
	Status                value.IndexGenerationStatus
	ModelName             string
	ModelDisplayName      string
	EmbeddingDimension    int
	ChunkerVersion        int
	ChunkingConfig        map[string]any
	RetrievalConfig       map[string]any
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

// KnowledgeBaseBlockerFacts contains resource identity only; the service owns user-facing messages.
type KnowledgeBaseBlockerFacts struct {
	Code                string
	ResourceType        string
	ResourceID          uuid.UUID
	ResourceDisplayName string
}

// KnowledgeBaseSummaryFacts is the persistence projection used to build a workbench summary.
type KnowledgeBaseSummaryFacts struct {
	KnowledgeBaseID     uuid.UUID
	KnowledgeBaseName   string
	ContentVersion      int64
	TotalDocuments      int64
	FileDocuments       int64
	FAQDocuments        int64
	WebDocuments        int64
	ReadyDocuments      int64
	ProcessingDocuments int64
	FailedDocuments     int64
	ActiveGeneration    *KnowledgeBaseGenerationFacts
	CandidateGeneration *KnowledgeBaseGenerationFacts
	HasUpdatingWork     bool
	RecentJobs          []KnowledgeBaseJobFacts
	Blockers            []KnowledgeBaseBlockerFacts
}

// KnowledgeBaseSummaryStore reads workbench facts inside one Workspace boundary.
type KnowledgeBaseSummaryStore interface {
	GetKnowledgeBaseSummaryFacts(context.Context, uuid.UUID, uuid.UUID) (*KnowledgeBaseSummaryFacts, error)
	ListKnowledgeBaseJobFacts(context.Context, uuid.UUID, uuid.UUID, KnowledgeBaseJobFactsFilter) ([]KnowledgeBaseJobFacts, error)
}

// KnowledgeBaseSummaryService builds safe, readable KnowledgeBase experience projections.
type KnowledgeBaseSummaryService struct{ store KnowledgeBaseSummaryStore }

// NewKnowledgeBaseSummaryService creates the KnowledgeBase experience query service.
func NewKnowledgeBaseSummaryService(store KnowledgeBaseSummaryStore) *KnowledgeBaseSummaryService {
	return &KnowledgeBaseSummaryService{store: store}
}

// GetSummary returns current content, index, blocker and recent activity facts.
func (s *KnowledgeBaseSummaryService) GetSummary(ctx context.Context, access value.ResourceAccess, knowledgeBaseID uuid.UUID) (*dto.KnowledgeBaseSummary, error) {
	workspaceID := access.WorkspaceID
	if s.store == nil {
		return nil, fmt.Errorf("%w: KnowledgeBase summary 参数无效", domainerrors.ErrValidation)
	}
	if err := validateResourceAccess(access, workspaceID, knowledgeBaseID); err != nil {
		return nil, err
	}
	facts, err := s.store.GetKnowledgeBaseSummaryFacts(ctx, workspaceID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if facts == nil {
		return nil, fmt.Errorf("%w: KnowledgeBase summary facts 为空", domainerrors.ErrConflict)
	}
	if facts.KnowledgeBaseID != knowledgeBaseID {
		return nil, domainerrors.ErrNotFound
	}
	active := generationSummary(facts.ActiveGeneration, true)
	candidate := generationSummary(facts.CandidateGeneration, false)
	recentCount := len(facts.RecentJobs)
	if recentCount > recentKnowledgeBaseJobLimit {
		recentCount = recentKnowledgeBaseJobLimit
	}
	recentJobs := make([]*dto.JobSummary, 0, recentCount)
	for index := 0; index < recentCount; index++ {
		recentJobs = append(recentJobs, jobSummary(facts.RecentJobs[index]))
	}
	blockers := make([]*dto.KnowledgeBaseBlocker, 0, len(facts.Blockers))
	for _, blocker := range facts.Blockers {
		name := readableResourceName(blocker.ResourceType, blocker.ResourceDisplayName)
		if blocker.ResourceType == "generation" && facts.CandidateGeneration != nil && blocker.ResourceID == facts.CandidateGeneration.ID && candidate != nil {
			name = candidate.DisplayLabel
		}
		blockers = append(blockers, &dto.KnowledgeBaseBlocker{
			Code: blocker.Code, ResourceType: blocker.ResourceType, ResourceID: blocker.ResourceID,
			ResourceDisplayName: name, Message: blockerMessage(blocker.Code),
		})
	}
	return &dto.KnowledgeBaseSummary{
		KnowledgeBaseID: knowledgeBaseID, KnowledgeBaseName: readableResourceName("knowledge_base", facts.KnowledgeBaseName),
		ContentVersion: facts.ContentVersion,
		DocumentCounts: dto.KnowledgeBaseDocumentCounts{
			Total: facts.TotalDocuments, File: facts.FileDocuments, FAQ: facts.FAQDocuments, Web: facts.WebDocuments,
			Ready: facts.ReadyDocuments, Processing: facts.ProcessingDocuments, Failed: facts.FailedDocuments,
		},
		ActiveGeneration: active, CandidateGeneration: candidate,
		SyncState: summarySyncState(*facts), RecentJobs: recentJobs, Blockers: blockers,
	}, nil
}

func summarySyncState(facts KnowledgeBaseSummaryFacts) dto.KnowledgeBaseSyncState {
	if facts.CandidateGeneration != nil && facts.CandidateGeneration.Status == value.IndexGenerationReady {
		return dto.KnowledgeBaseSyncCandidateReady
	}
	if facts.FailedDocuments > 0 || generationFailed(facts.CandidateGeneration) || hasFailureBlocker(facts.Blockers) {
		return dto.KnowledgeBaseSyncFailed
	}
	if facts.HasUpdatingWork || (facts.CandidateGeneration != nil && facts.CandidateGeneration.Status == value.IndexGenerationBuilding) {
		return dto.KnowledgeBaseSyncUpdating
	}
	return dto.KnowledgeBaseSyncSynced
}

func generationFailed(facts *KnowledgeBaseGenerationFacts) bool {
	return facts != nil && (facts.Status == value.IndexGenerationFailed || facts.Status == value.IndexGenerationStale)
}

func hasFailureBlocker(blockers []KnowledgeBaseBlockerFacts) bool {
	for _, blocker := range blockers {
		if blocker.Code != "" {
			return true
		}
	}
	return false
}

func generationSummary(facts *KnowledgeBaseGenerationFacts, active bool) *dto.KnowledgeBaseGenerationSummary {
	if facts == nil {
		return nil
	}
	modelName := strings.TrimSpace(facts.ModelDisplayName)
	if modelName == "" {
		modelName = strings.TrimSpace(facts.ModelName)
	}
	if modelName == "" {
		modelName = "未命名模型"
	}
	statusLabel := generationStatusLabel(facts.Status, active)
	return &dto.KnowledgeBaseGenerationSummary{
		ID: facts.ID, DisplayLabel: fmt.Sprintf("%s · %s · %s", facts.CreatedAt.Format("2006-01-02 15:04"), modelName, statusLabel),
		Status: facts.Status, ModelDisplayName: modelName, EmbeddingDimension: facts.EmbeddingDimension,
		ChunkerVersion: facts.ChunkerVersion, ChunkingConfig: cloneSummaryMap(facts.ChunkingConfig), RetrievalConfig: cloneSummaryMap(facts.RetrievalConfig),
		SourceContentVersion: facts.SourceContentVersion, IndexedContentVersion: facts.IndexedContentVersion,
		DocumentCount: facts.DocumentCount, ChunkCount: facts.ChunkCount, IndexedCount: facts.IndexedCount,
		ManualEditCount: facts.ManualEditCount, DisabledChunkCount: facts.DisabledChunkCount,
		ErrorMessage: safeGenerationError(facts.Status), CreatedAt: facts.CreatedAt, ReadyAt: facts.ReadyAt, ActivatedAt: facts.ActivatedAt,
	}
}

func generationStatusLabel(status value.IndexGenerationStatus, active bool) string {
	if active && status == value.IndexGenerationReady {
		return "当前生效"
	}
	switch status {
	case value.IndexGenerationBuilding:
		return "构建中"
	case value.IndexGenerationReady:
		return "待激活"
	case value.IndexGenerationStale:
		return "已过期"
	case value.IndexGenerationFailed:
		return "构建失败"
	case value.IndexGenerationRetired:
		return "已退役"
	default:
		return "状态未知"
	}
}

func safeGenerationError(status value.IndexGenerationStatus) string {
	switch status {
	case value.IndexGenerationFailed:
		return "索引版本构建失败，请检查模型配置后重新构建。"
	case value.IndexGenerationStale:
		return "内容已更新，此索引版本需要重新构建。"
	default:
		return ""
	}
}

func blockerMessage(code string) string {
	switch code {
	case "document_processing_failed":
		return "内容处理失败，请查看任务并重新导入或删除内容。"
	case "generation_build_failed":
		return "索引版本构建失败，请检查模型配置后重新构建。"
	case "generation_stale":
		return "内容已更新，此索引版本已经过期，请重新构建。"
	case "active_model_unavailable":
		return "当前索引使用的模型不可用，请启用模型或构建新的索引版本。"
	default:
		return "此资源当前阻止知识库完成同步，请检查相关配置。"
	}
}

func readableResourceName(resourceType, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	switch resourceType {
	case "knowledge_base":
		return "未命名知识库"
	case "generation":
		return "未命名索引版本"
	case "model":
		return "未命名模型"
	default:
		return "未命名文档"
	}
}

func cloneSummaryMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}
