package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/requestmeta"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// searchRunStats 收集一次检索执行的阶段统计，用于 terminal slog event。
type searchRunStats struct {
	startedAt  time.Time
	queryChars int
	err        error
	rerankEnabled,
	rerankApplied,
	rerankFallback bool
	rankingStage string
	// 召回与重排阶段计数，证明重排实际参与排序。
	vectorCandidateCount,
	keywordCandidateCount,
	fusedCandidateCount,
	groupedCandidateCount,
	rerankCandidateCount,
	resultCount int
}

// logTerminal 根据最终 error 记录唯一的 search.completed 或 search.failed 事件。
// 日志字段严格使用 spec 第 14 节 allowlist；不记录 query、候选正文、向量或凭证。
func (s *SearchService) logTerminal(ctx context.Context, stats *searchRunStats, input SearchInput, _ string) {
	if stats == nil {
		return
	}
	meta := requestmeta.From(ctx)
	totalMS := time.Since(stats.startedAt).Milliseconds()
	attrs := []any{
		slog.String("event", "search.completed"),
		slog.String("request_id", meta.RequestID),
		slog.String("transport", meta.Transport),
		slog.String("principal_kind", meta.PrincipalKind),
		slog.String("workspace_id", input.WorkspaceID.String()),
		slog.String("knowledge_base_id", input.KnowledgeBaseID.String()),
		slog.Int("query_chars", stats.queryChars),
		slog.Int("vector_candidate_count", stats.vectorCandidateCount),
		slog.Int("keyword_candidate_count", stats.keywordCandidateCount),
		slog.Int("fused_candidate_count", stats.fusedCandidateCount),
		slog.Int("grouped_candidate_count", stats.groupedCandidateCount),
		slog.Bool("rerank_enabled", stats.rerankEnabled),
		slog.Bool("rerank_applied", stats.rerankApplied),
		slog.Bool("rerank_fallback", stats.rerankFallback),
		slog.Int("rerank_candidate_count", stats.rerankCandidateCount),
		slog.Int("result_count", stats.resultCount),
		slog.String("ranking_stage", stats.rankingStage),
		slog.Int64("total_duration_ms", totalMS),
	}
	if stats.err != nil {
		attrs[0] = slog.String("event", "search.failed")
		level := slog.LevelWarn
		if isInternalError(stats.err) {
			level = slog.LevelError
		}
		attrs = append(attrs, slog.String("error_class", errorClassOf(stats.err)))
		s.logger.Log(ctx, level, "search.failed", attrs...)
		return
	}
	s.logger.InfoContext(ctx, "search.completed", attrs...)
}

// errorClassOf 把领域错误映射为稳定、脱敏的 error class。
func errorClassOf(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, domainerrors.ErrRerankConfigurationConflict):
		return "rerank_configuration_conflict"
	case errors.Is(err, domainerrors.ErrRerankSnapshotMismatch):
		return "rerank_snapshot_mismatch"
	case errors.Is(err, domainerrors.ErrRerankUnavailable):
		return "rerank_unavailable"
	case errors.Is(err, domainerrors.ErrRerankRateLimited):
		return "rerank_rate_limited"
	case errors.Is(err, domainerrors.ErrInvalidRerankResponse):
		return "invalid_rerank_response"
	case errors.Is(err, domainerrors.ErrRerankInputTooLarge):
		return "rerank_input_too_large"
	case errors.Is(err, domainerrors.ErrValidation):
		return "validation_error"
	case errors.Is(err, domainerrors.ErrGenerationNotReady):
		return "generation_not_ready"
	case errors.Is(err, domainerrors.ErrGenerationStale):
		return "generation_stale"
	case errors.Is(err, domainerrors.ErrForbidden), errors.Is(err, domainerrors.ErrUnauthorized):
		return "forbidden"
	case errors.Is(err, domainerrors.ErrNotFound):
		return "not_found"
	default:
		return "internal_error"
	}
}

// isInternalError 判断是否为不可恢复的内部错误（应记 Error）。
func isInternalError(err error) bool {
	if err == nil {
		return false
	}
	for _, sentinel := range []error{
		domainerrors.ErrInvalidRerankResponse,
		domainerrors.ErrRerankUnavailable,
		domainerrors.ErrRerankRateLimited,
		domainerrors.ErrRerankInputTooLarge,
		domainerrors.ErrRerankConfigurationConflict,
		domainerrors.ErrRerankSnapshotMismatch,
		domainerrors.ErrValidation,
		domainerrors.ErrForbidden,
		domainerrors.ErrUnauthorized,
		domainerrors.ErrNotFound,
		domainerrors.ErrGenerationNotReady,
		domainerrors.ErrGenerationStale,
	} {
		if errors.Is(err, sentinel) {
			return false
		}
	}
	return true
}

var _ = uuid.Nil
