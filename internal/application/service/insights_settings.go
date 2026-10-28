package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// QueryLogSettingsStore 是查询原文开关的持久化接口。
type QueryLogSettingsStore interface {
	GetQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID) (bool, error)
	UpdateQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID, enabled bool) error
}

// QueryLogSettingsService 管理 knowledge_bases.query_log_enabled（D1 opt-in 开关）。
type QueryLogSettingsService struct {
	store QueryLogSettingsStore
	kbs   KnowledgeBaseReader
}

// NewQueryLogSettingsService creates a query log settings service.
func NewQueryLogSettingsService(store QueryLogSettingsStore, kbs KnowledgeBaseReader) *QueryLogSettingsService {
	return &QueryLogSettingsService{store: store, kbs: kbs}
}

// UpdateQueryLogEnabled 更新开关；跨 workspace / KB 不存在返回 ErrNotFound。
func (s *QueryLogSettingsService) UpdateQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID, enabled bool) error {
	if _, err := s.kbs.Get(ctx, workspaceID, kbID); err != nil {
		return err
	}
	return s.store.UpdateQueryLogEnabled(ctx, workspaceID, kbID, enabled)
}

// GetQueryLogEnabled 读取开关。
func (s *QueryLogSettingsService) GetQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID) (bool, error) {
	if _, err := s.kbs.Get(ctx, workspaceID, kbID); err != nil {
		return false, err
	}
	return s.store.GetQueryLogEnabled(ctx, workspaceID, kbID)
}

// CitationSummaryStore 是文档引用摘要的持久化接口。
type CitationSummaryStore interface {
	DocumentCitationSummary(ctx context.Context, workspaceID, kbID, documentID uuid.UUID, since time.Time) (int64, *time.Time, error)
}

// CitationSummaryService 提供删除预警所需的文档引用摘要（prog 组）。
type CitationSummaryService struct {
	store CitationSummaryStore
	kbs   KnowledgeBaseReader
}

// NewCitationSummaryService creates a citation summary service.
func NewCitationSummaryService(store CitationSummaryStore, kbs KnowledgeBaseReader) *CitationSummaryService {
	return &CitationSummaryService{store: store, kbs: kbs}
}

// DocumentCitationSummary 返回窗口内引用次数与最近引用时间；KB 越界返回 ErrNotFound。
// 文档已删时引用行仍在（D3），摘要照常返回——删除后的审计同理。
func (s *CitationSummaryService) DocumentCitationSummary(ctx context.Context, workspaceID, kbID, documentID uuid.UUID, windowDays int) (int64, *time.Time, error) {
	if _, err := s.kbs.Get(ctx, workspaceID, kbID); err != nil {
		return 0, nil, err
	}
	if err := value.ValidateInsightsWindowDays(windowDays); err != nil {
		return 0, nil, err
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays)
	return s.store.DocumentCitationSummary(ctx, workspaceID, kbID, documentID, since)
}
