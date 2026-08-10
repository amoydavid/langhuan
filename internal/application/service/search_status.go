package service

import (
	"errors"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// searchFailurePhase 标识检索失败发生的阶段，用于阶段感知的失败分类。
type searchFailurePhase string

const (
	searchFailurePhaseValidation searchFailurePhase = "validation"
	searchFailurePhaseEmbedding  searchFailurePhase = "embedding"
	searchFailurePhaseRetrieval  searchFailurePhase = "retrieval"
	searchFailurePhaseRerank     searchFailurePhase = "rerank"
)

// classifySearchFailure 把领域错误结合发生阶段映射为稳定、脱敏的 failure_class。
// 该分类器同时被 SearchRun 记录器和日志/Trace 使用，保证同一错误在协议和日志中一致。
func classifySearchFailure(err error, phase searchFailurePhase) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, domainerrors.ErrValidation):
		return "validation_error"
	case errors.Is(err, domainerrors.ErrNotFound):
		return "not_found"
	case errors.Is(err, domainerrors.ErrForbidden), errors.Is(err, domainerrors.ErrUnauthorized):
		return "forbidden"
	case errors.Is(err, domainerrors.ErrGenerationNotReady):
		return "generation_not_ready"
	case errors.Is(err, domainerrors.ErrGenerationStale):
		return "generation_stale"
	case errors.Is(err, domainerrors.ErrEmbeddingSnapshotMismatch):
		return "embedding_snapshot_mismatch"
	case errors.Is(err, domainerrors.ErrRerankSnapshotMismatch):
		return "rerank_snapshot_mismatch"
	case errors.Is(err, domainerrors.ErrRerankConfigurationConflict):
		return "rerank_configuration_conflict"
	case errors.Is(err, domainerrors.ErrRerankInputTooLarge):
		return "rerank_input_too_large"
	case errors.Is(err, domainerrors.ErrInvalidEmbeddingResponse):
		if phase == searchFailurePhaseEmbedding {
			return "invalid_embedding_response"
		}
		return "invalid_embedding_response"
	case errors.Is(err, domainerrors.ErrInvalidRerankResponse):
		return "invalid_rerank_response"
	}

	if phase == searchFailurePhaseEmbedding {
		switch {
		case errors.Is(err, domainerrors.ErrEndpointUnreachable):
			return "embedding_unavailable"
		case errors.Is(err, domainerrors.ErrRequestTimeout):
			return "embedding_timeout"
		case errors.Is(err, domainerrors.ErrRateLimited):
			return "embedding_rate_limited"
		}
	}

	if phase == searchFailurePhaseRerank {
		switch {
		case errors.Is(err, domainerrors.ErrRerankUnavailable), errors.Is(err, domainerrors.ErrEndpointUnreachable):
			return "rerank_unavailable"
		case errors.Is(err, domainerrors.ErrRerankRateLimited), errors.Is(err, domainerrors.ErrRateLimited):
			return "rerank_rate_limited"
		case errors.Is(err, domainerrors.ErrRequestTimeout):
			return "rerank_timeout"
		}
	}

	return "internal_error"
}
