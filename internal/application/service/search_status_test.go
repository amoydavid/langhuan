package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestClassifySearchFailureByPhase(t *testing.T) {
	require.Equal(t, "embedding_timeout",
		classifySearchFailure(domainerrors.ErrRequestTimeout, searchFailurePhaseEmbedding))
	require.Equal(t, "rerank_timeout",
		classifySearchFailure(domainerrors.ErrRequestTimeout, searchFailurePhaseRerank))
	require.Equal(t, "generation_not_ready",
		classifySearchFailure(domainerrors.ErrGenerationNotReady, searchFailurePhaseRetrieval))
}

func TestClassifySearchFailureEmbeddingPhase(t *testing.T) {
	require.Equal(t, "embedding_unavailable",
		classifySearchFailure(domainerrors.ErrEndpointUnreachable, searchFailurePhaseEmbedding))
	require.Equal(t, "embedding_rate_limited",
		classifySearchFailure(domainerrors.ErrRateLimited, searchFailurePhaseEmbedding))
	require.Equal(t, "embedding_snapshot_mismatch",
		classifySearchFailure(domainerrors.ErrEmbeddingSnapshotMismatch, searchFailurePhaseEmbedding))
	require.Equal(t, "invalid_embedding_response",
		classifySearchFailure(domainerrors.ErrInvalidEmbeddingResponse, searchFailurePhaseEmbedding))
}

func TestClassifySearchFailureRerankPhase(t *testing.T) {
	require.Equal(t, "rerank_unavailable",
		classifySearchFailure(domainerrors.ErrRerankUnavailable, searchFailurePhaseRerank))
	require.Equal(t, "rerank_rate_limited",
		classifySearchFailure(domainerrors.ErrRerankRateLimited, searchFailurePhaseRerank))
	require.Equal(t, "rerank_snapshot_mismatch",
		classifySearchFailure(domainerrors.ErrRerankSnapshotMismatch, searchFailurePhaseRerank))
	require.Equal(t, "invalid_rerank_response",
		classifySearchFailure(domainerrors.ErrInvalidRerankResponse, searchFailurePhaseRerank))
}

func TestClassifySearchFailureCommon(t *testing.T) {
	require.Equal(t, "validation_error",
		classifySearchFailure(domainerrors.ErrValidation, searchFailurePhaseValidation))
	require.Equal(t, "not_found",
		classifySearchFailure(domainerrors.ErrNotFound, searchFailurePhaseRetrieval))
	require.Equal(t, "forbidden",
		classifySearchFailure(domainerrors.ErrForbidden, searchFailurePhaseRetrieval))
	require.Equal(t, "generation_stale",
		classifySearchFailure(domainerrors.ErrGenerationStale, searchFailurePhaseRetrieval))
}

func TestClassifySearchFailureUnknownIsInternal(t *testing.T) {
	require.Equal(t, "internal_error", classifySearchFailure(errors.New("boom"), searchFailurePhaseRetrieval))
}

func TestClassifySearchFailureNilIsEmpty(t *testing.T) {
	require.Equal(t, "", classifySearchFailure(nil, searchFailurePhaseRetrieval))
}
