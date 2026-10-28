package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchKBFlagReader 读取检索遥测相关的 KB 级开关。由 infrastructure/db 实现。
type SearchKBFlagReader interface {
	// GetQueryLogEnabled 返回 KB 的查询原文记录开关；KB 不存在返回 ErrNotFound。
	GetQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID) (bool, error)
	// GetQueryLogEnabledBatch 批量读取开关（单条 IN 查询）；不存在的 KB 不在结果中。
	// 多库检索投递必须走批量，避免逐库点查放大热路径。
	GetQueryLogEnabledBatch(ctx context.Context, workspaceID uuid.UUID, kbIDs []uuid.UUID) (map[uuid.UUID]bool, error)
}

// buildCitationEntries 把最终返回的结果行构造为引用遥测（spec §4.2）。
// 只在 available/degraded 且有结果行时产出；rank 按返回顺序从 1 起。
// 单库调用方需传入 kbID（多库结果行自带 KnowledgeBaseID，传 uuid.Nil）。
func buildCitationEntries(
	workspaceID, fallbackKbID, runID uuid.UUID,
	results []*dto.SearchResult,
	status value.RetrievalStatus,
	transport string,
	now time.Time,
) []model.SearchCitation {
	if len(results) == 0 {
		return nil
	}
	if status != value.RetrievalStatusAvailable && status != value.RetrievalStatusDegraded {
		return nil
	}
	entries := make([]model.SearchCitation, 0, len(results))
	for rank, result := range results {
		if result == nil {
			continue
		}
		kbID := fallbackKbID
		if kbID == uuid.Nil {
			kbID = result.KnowledgeBaseID
		}
		if kbID == uuid.Nil || result.DocumentID == uuid.Nil || result.ChunkRevisionID == uuid.Nil {
			continue
		}
		entries = append(entries, model.SearchCitation{
			ID: uuid.New(), WorkspaceID: workspaceID, KbID: kbID,
			SearchRunID: runID, DocumentID: result.DocumentID,
			ChunkRevisionID: result.ChunkRevisionID,
			Rank:            rank + 1, RetrievalStatus: status, CreatedAt: now,
		})
	}
	return entries
}

// buildQueryLogEntry 构造一条查询原文日志。仅由开关开启后的调用方使用；
// failed 的 run 不投递（错误分类已由 failure_class 承载）。
func buildQueryLogEntry(
	workspaceID, kbID, runID uuid.UUID,
	query, queryHash string,
	status value.RetrievalStatus,
	now time.Time,
) model.SearchQueryLogEntry {
	return model.SearchQueryLogEntry{
		ID: uuid.New(), WorkspaceID: workspaceID, KbID: kbID, SearchRunID: runID,
		QueryText: query, QueryHash: queryHash, QueryChars: len([]rune(query)),
		RetrievalStatus: status, CreatedAt: now,
	}
}

// recordSingleKBTelemetry 单库检索的投递入口：Finish 之后调用，非阻塞、不返回错误。
// 开关读取失败降级为不记明文（保守）；遥测整体未装配时为 no-op。
func recordSingleKBTelemetry(
	ctx context.Context,
	recorder *CitationRecorder,
	flags SearchKBFlagReader,
	logger *slog.Logger,
	workspaceID, kbID, runID uuid.UUID,
	query, queryHash, transport string,
	results []*dto.SearchResult,
	status value.RetrievalStatus,
) {
	if recorder == nil {
		return
	}
	now := time.Now().UTC()
	citations := buildCitationEntries(workspaceID, kbID, runID, results, status, transport, now)
	var logs []model.SearchQueryLogEntry
	if queryLogEnabled(ctx, logger, flags, workspaceID, kbID) {
		logs = append(logs, buildQueryLogEntry(workspaceID, kbID, runID, query, queryHash, status, now))
	}
	recorder.RecordCitations(citations, logs)
}

// recordMultiKBTelemetry 多库检索的投递入口：引用行按各结果行自带 KB 归属；
// 查询原文对每个开启开关的参与库各记一行（与 v0.6.0 多库语义一致）。
func recordMultiKBTelemetry(
	ctx context.Context,
	recorder *CitationRecorder,
	flags SearchKBFlagReader,
	logger *slog.Logger,
	workspaceID, runID uuid.UUID,
	query, queryHash, transport string,
	results []*dto.SearchResult,
	kbIDs []uuid.UUID,
	status value.RetrievalStatus,
) {
	if recorder == nil {
		return
	}
	now := time.Now().UTC()
	citations := buildCitationEntries(workspaceID, uuid.Nil, runID, results, status, transport, now)
	var logs []model.SearchQueryLogEntry
	// 批量读开关（单条 IN 查询）：多库投递不做逐库点查。
	if enabledByKB, err := flags.GetQueryLogEnabledBatch(ctx, workspaceID, kbIDs); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("insights_query_log_flag_read_failed",
			slog.String("event", "insights_query_log_flag_read_failed"),
			slog.String("error", err.Error()),
		)
	} else {
		for _, kbID := range kbIDs {
			if enabledByKB[kbID] {
				logs = append(logs, buildQueryLogEntry(workspaceID, kbID, runID, query, queryHash, status, now))
			}
		}
	}
	recorder.RecordCitations(citations, logs)
}

// queryLogEnabled 读取 KB 开关；读取失败降级为 false（不记明文）并记日志。
// 降级不影响引用遥测——两者独立。
func queryLogEnabled(ctx context.Context, logger *slog.Logger, flags SearchKBFlagReader, workspaceID, kbID uuid.UUID) bool {
	if flags == nil {
		return false
	}
	enabled, err := flags.GetQueryLogEnabled(ctx, workspaceID, kbID)
	if err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("insights_query_log_flag_read_failed",
			slog.String("event", "insights_query_log_flag_read_failed"),
			slog.String("error", err.Error()),
		)
		return false
	}
	return enabled
}
