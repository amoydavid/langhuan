package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 聚合行视图类型：由 infrastructure/db 的 InsightsQueryRepository 用 GORM Raw 扫描填充
// （db 依赖 application 是既有方向；application 不 import infrastructure）。

// InsightsRunStatusCount 是一个窗口内某检索状态的 run 数（按 KB 参与维度）。
type InsightsRunStatusCount struct {
	RetrievalStatus string
	Runs            int64
}

// InsightsZeroRecallGroup 是零召回查询的 hash 聚合组。
type InsightsZeroRecallGroup struct {
	QueryHash  string
	QueryChars int
	Hits       int64
	FirstSeen  time.Time
	LastSeen   time.Time
}

// InsightsLatestQueryText 是一个 query_hash 在 query_log 中的最新原文。
type InsightsLatestQueryText struct {
	QueryHash string
	QueryText string
}

// InsightsQueryStore 是健康面板聚合所需的只读持久化接口（infrastructure/db 实现）。
type InsightsQueryStore interface {
	CountRunsByStatus(ctx context.Context, workspaceID, kbID uuid.UUID, from, to time.Time) ([]InsightsRunStatusCount, error)
	ZeroRecallGroups(ctx context.Context, workspaceID, kbID uuid.UUID, from, to time.Time, limit int) ([]InsightsZeroRecallGroup, error)
	LatestQueryTexts(ctx context.Context, workspaceID, kbID uuid.UUID, from time.Time) ([]InsightsLatestQueryText, error)
	SleepingDocuments(ctx context.Context, workspaceID, kbID uuid.UUID, windowStart time.Time, limit int) ([]dto.InsightsSleepingDocument, error)
	RecentActivity(ctx context.Context, workspaceID, kbID uuid.UUID, since time.Time, limit int) ([]dto.InsightsActivityEvent, error)
}

// InsightsRunStatusCount 等聚合行类型由 db 包定义；为避免 application 依赖 infrastructure，
// 在此处声明等价视图并由 db 侧装配转换（见 db.InsightsServiceAdapter）。

// SleepingSampleMinRuns 是沉睡清单的样本下限护栏（spec §7）：窗口内完成 run 数
// 低于该值时零引用不构成"沉睡"证据（零流量知识库会把全部内容误报为沉睡）。
const SleepingSampleMinRuns = 10

// InsightsQueryService 编排健康面板的四类聚合（spec §4.5 口径 + §7 响应形状）。
type InsightsQueryService struct {
	store  InsightsQueryStore
	flags  SearchKBFlagReader
	kbs    KnowledgeBaseReader
	logger *slog.Logger
	now    func() time.Time
}

// NewInsightsQueryService creates an insights query service.
func NewInsightsQueryService(store InsightsQueryStore, flags SearchKBFlagReader, kbs KnowledgeBaseReader, logger *slog.Logger) *InsightsQueryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &InsightsQueryService{store: store, flags: flags, kbs: kbs, logger: logger, now: time.Now}
}

// InsightsOverview 是健康面板总览（spec §7 overview 响应）。
type InsightsOverview struct {
	WindowDays               int     `json:"window_days"`
	RunsTotal                int64   `json:"runs_total"`
	RunsAvailable            int64   `json:"runs_available"`
	RunsEmpty                int64   `json:"runs_empty"`
	RunsDegraded             int64   `json:"runs_degraded"`
	RunsFailedExcluded       int64   `json:"runs_failed_excluded"`
	EmptyRecallRate          float64 `json:"empty_recall_rate"`
	PreviousEmptyRecallRate  float64 `json:"previous_empty_recall_rate"`
	HasPreviousWindow        bool    `json:"has_previous_window"`
	SleepingDocumentsCount   int64   `json:"sleeping_documents_count"`
	SleepingInsufficientData bool    `json:"sleeping_insufficient_sample"`
	RecentChangesCount       int64   `json:"recent_changes_count"`
	QueryLogEnabled          bool    `json:"query_log_enabled"`
}

// InsightsZeroRecallQuery 是零召回面板的一行。
type InsightsZeroRecallQuery struct {
	QueryHash       string    `json:"query_hash"`
	QueryChars      int       `json:"query_chars"`
	Hits            int64     `json:"hits"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	LatestQueryText string    `json:"latest_query_text,omitempty"`
}

// Overview 计算窗口总览（含环比与沉睡计数；沉睡口径固定 30 天样本护栏）。
func (s *InsightsQueryService) Overview(ctx context.Context, workspaceID, kbID uuid.UUID, windowDays int) (*InsightsOverview, error) {
	if err := value.ValidateInsightsWindowDays(windowDays); err != nil {
		return nil, err
	}
	if err := s.ensureKB(ctx, workspaceID, kbID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	windowFrom := now.AddDate(0, 0, -windowDays)
	prevFrom := windowFrom.AddDate(0, 0, -windowDays)

	current, err := s.store.CountRunsByStatus(ctx, workspaceID, kbID, windowFrom, now)
	if err != nil {
		return nil, err
	}
	overview := &InsightsOverview{WindowDays: windowDays}
	for _, row := range current {
		switch row.RetrievalStatus {
		case "available":
			overview.RunsAvailable = row.Runs
		case "empty":
			overview.RunsEmpty = row.Runs
		case "degraded":
			overview.RunsDegraded = row.Runs
		case "failed":
			overview.RunsFailedExcluded = row.Runs
		}
	}
	overview.RunsTotal = overview.RunsAvailable + overview.RunsEmpty + overview.RunsDegraded
	overview.EmptyRecallRate = emptyRate(overview.RunsEmpty, overview.RunsTotal)

	previous, err := s.store.CountRunsByStatus(ctx, workspaceID, kbID, prevFrom, windowFrom)
	if err != nil {
		return nil, err
	}
	var prevEmpty, prevTotal int64
	for _, row := range previous {
		switch row.RetrievalStatus {
		case "empty":
			prevEmpty = row.Runs
		case "available", "degraded":
			prevTotal += row.Runs
		}
	}
	if prevTotal > 0 {
		overview.HasPreviousWindow = true
		overview.PreviousEmptyRecallRate = emptyRate(prevEmpty, prevTotal)
	}

	// 沉睡计数：口径固定 30 天窗口 + 样本护栏。
	sleepingFrom := now.AddDate(0, 0, -30)
	sleeping, err := s.store.SleepingDocuments(ctx, workspaceID, kbID, sleepingFrom, value.InsightsListMaxLimit)
	if err != nil {
		return nil, err
	}
	if overview.RunsTotal < SleepingSampleMinRuns {
		overview.SleepingInsufficientData = true
		overview.SleepingDocumentsCount = 0
	} else {
		overview.SleepingDocumentsCount = int64(len(sleeping))
	}
	// 近窗变更计数（activity 段数，仅计数不做上限展开）。
	activity, err := s.store.RecentActivity(ctx, workspaceID, kbID, windowFrom, value.InsightsActivityMaxLimit)
	if err != nil {
		return nil, err
	}
	overview.RecentChangesCount = int64(len(activity))

	if enabled, err := s.flags.GetQueryLogEnabled(ctx, workspaceID, kbID); err == nil {
		overview.QueryLogEnabled = enabled
	}
	return overview, nil
}

// ZeroRecall 返回零召回查询组；query_log 开启时附每组最新原文。
func (s *InsightsQueryService) ZeroRecall(ctx context.Context, workspaceID, kbID uuid.UUID, windowDays, limit int) ([]InsightsZeroRecallQuery, bool, error) {
	if err := value.ValidateInsightsWindowDays(windowDays); err != nil {
		return nil, false, err
	}
	if err := value.ValidateInsightsListLimit(limit); err != nil {
		return nil, false, err
	}
	if err := s.ensureKB(ctx, workspaceID, kbID); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	windowFrom := now.AddDate(0, 0, -windowDays)

	groups, err := s.store.ZeroRecallGroups(ctx, workspaceID, kbID, windowFrom, now, limit)
	if err != nil {
		return nil, false, err
	}
	result := make([]InsightsZeroRecallQuery, 0, len(groups))
	for _, group := range groups {
		result = append(result, InsightsZeroRecallQuery{
			QueryHash: group.QueryHash, QueryChars: group.QueryChars,
			Hits: group.Hits, FirstSeenAt: group.FirstSeen, LastSeenAt: group.LastSeen,
		})
	}

	enabled, err := s.flags.GetQueryLogEnabled(ctx, workspaceID, kbID)
	if err != nil {
		return result, false, nil
	}
	if enabled && len(result) > 0 {
		texts, err := s.store.LatestQueryTexts(ctx, workspaceID, kbID, windowFrom)
		if err != nil {
			s.logger.WarnContext(ctx, "insights_query_log_texts_failed",
				slog.String("event", "insights_query_log_texts_failed"),
				slog.String("error", err.Error()),
			)
			return result, enabled, nil
		}
		byHash := make(map[string]string, len(texts))
		for _, text := range texts {
			if _, exists := byHash[text.QueryHash]; !exists {
				byHash[text.QueryHash] = text.QueryText
			}
		}
		for i := range result {
			result[i].LatestQueryText = byHash[result[i].QueryHash]
		}
	}
	return result, enabled, nil
}

// Sleeping 返回沉睡内容清单（固定 30 天口径）；样本不足时返回 insufficient=true。
func (s *InsightsQueryService) Sleeping(ctx context.Context, workspaceID, kbID uuid.UUID, limit int) ([]dto.InsightsSleepingDocument, bool, error) {
	if err := value.ValidateInsightsListLimit(limit); err != nil {
		return nil, false, err
	}
	if err := s.ensureKB(ctx, workspaceID, kbID); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	windowFrom := now.AddDate(0, 0, -30)

	// 样本护栏：窗口内完成 run 数不足时不出清单。
	counts, err := s.store.CountRunsByStatus(ctx, workspaceID, kbID, windowFrom, now)
	if err != nil {
		return nil, false, err
	}
	var completed int64
	for _, row := range counts {
		if row.RetrievalStatus != "failed" {
			completed += row.Runs
		}
	}
	if completed < SleepingSampleMinRuns {
		return nil, true, nil
	}

	rows, err := s.store.SleepingDocuments(ctx, workspaceID, kbID, windowFrom, limit)
	if err != nil {
		return nil, false, err
	}
	result := make([]dto.InsightsSleepingDocument, 0, len(rows))
	for _, row := range rows {
		result = append(result, dto.InsightsSleepingDocument{
			DocumentID: row.DocumentID, Kind: row.Kind,
			Title: row.Title, IndexedAt: row.IndexedAt,
		})
	}
	return result, false, nil
}

// Activity 返回内容动态 feed。
func (s *InsightsQueryService) Activity(ctx context.Context, workspaceID, kbID uuid.UUID, limit int) ([]dto.InsightsActivityEvent, error) {
	if err := value.ValidateInsightsActivityLimit(limit); err != nil {
		return nil, err
	}
	if err := s.ensureKB(ctx, workspaceID, kbID); err != nil {
		return nil, err
	}
	since := s.now().UTC().AddDate(0, 0, -30)
	events, err := s.store.RecentActivity(ctx, workspaceID, kbID, since, limit)
	if err != nil {
		return nil, err
	}
	result := make([]dto.InsightsActivityEvent, 0, len(events))
	for _, event := range events {
		result = append(result, dto.InsightsActivityEvent{
			Kind: event.Kind, TargetDocumentID: event.TargetDocumentID,
			SourceID: event.SourceID, SourceName: event.SourceName,
			DocumentKind: event.DocumentKind, DocumentTitle: event.DocumentTitle,
			RevisionReason: event.RevisionReason, Success: event.Success,
			ErrorMessage: event.ErrorMessage, OccurredAt: event.OccurredAt,
		})
	}
	return result, nil
}

func (s *InsightsQueryService) ensureKB(ctx context.Context, workspaceID, kbID uuid.UUID) error {
	if s.kbs == nil {
		return nil
	}
	_, err := s.kbs.Get(ctx, workspaceID, kbID)
	return err // Get 对跨 workspace/不存在统一映射 ErrNotFound（既有合同）
}

// emptyRate 计算空召回率：empty / (available + empty + degraded)。
// 分母为 0 时返回 0（无样本不构成比率）。
func emptyRate(empty, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(empty) / float64(total)
}
