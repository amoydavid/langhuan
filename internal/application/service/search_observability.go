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
	rerankModelID, rerankProviderID uuid.UUID
	rankingStage                    string
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
		slog.String("rerank_profile", "default"),
		slog.String("rerank_model_id", stats.rerankModelID.String()),
		slog.String("rerank_provider_id", stats.rerankProviderID.String()),
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
// 与 SearchRun 记录器复用同一 classifier，保证协议和日志一致。
// 日志记录发生在检索结束后，阶段不可恢复时按 retrieval 兜底分类。
func errorClassOf(err error) string {
	return classifySearchFailure(err, searchFailurePhaseRetrieval)
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
		domainerrors.ErrEmbeddingSnapshotMismatch,
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
