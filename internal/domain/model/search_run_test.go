package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func validSearchRun() *SearchRun {
	now := time.Now()
	return &SearchRun{
		ID:              uuid.New(),
		WorkspaceID:     uuid.New(),
		RequestedScope:  value.SearchScopeSelected,
		QueryHash:       "sha256:v1:abc",
		QueryChars:      4,
		VectorTopK:      20,
		KeywordTopK:     20,
		FinalTopK:       10,
		RetrievalStatus: value.RetrievalStatusAvailable,
		RankingStage:    value.RankingStageRRF,
		ResultCount:     2,
		CreatedAt:       now,
		ExpiresAt:       now.Add(168 * time.Hour),
	}
}

func TestSearchRunValidateAcceptsValid(t *testing.T) {
	require.NoError(t, validSearchRun().Validate())
}

func TestSearchRunValidateRejectsFailedWithoutFailureClass(t *testing.T) {
	run := validSearchRun()
	run.RetrievalStatus = value.RetrievalStatusFailed
	run.FailureClass = ""
	require.ErrorIs(t, run.Validate(), domainerrors.ErrValidation)
}

func TestSearchRunValidateRejectsNonFailedWithFailureClass(t *testing.T) {
	run := validSearchRun()
	run.RetrievalStatus = value.RetrievalStatusAvailable
	run.FailureClass = "internal_error"
	require.ErrorIs(t, run.Validate(), domainerrors.ErrValidation)
}

func TestSearchRunValidateRejectsBadScope(t *testing.T) {
	run := validSearchRun()
	run.RequestedScope = value.SearchScope("bad")
	require.ErrorIs(t, run.Validate(), domainerrors.ErrValidation)
}

func TestSearchRunGenerationValidateRejectsEmptyHashes(t *testing.T) {
	gen := &SearchRunGeneration{
		ID: uuid.New(), WorkspaceID: uuid.New(), SearchRunID: uuid.New(),
		KnowledgeBaseID: uuid.New(), GenerationID: uuid.New(),
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(),
		EmbeddingDimension: 1024,
	}
	require.ErrorIs(t, gen.Validate(), domainerrors.ErrValidation)
}
