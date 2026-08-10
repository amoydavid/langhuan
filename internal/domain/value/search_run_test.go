package value

import (
	"testing"

	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestRetrievalStatusValidate(t *testing.T) {
	valid := []RetrievalStatus{
		RetrievalStatusRunning,
		RetrievalStatusAvailable,
		RetrievalStatusEmpty,
		RetrievalStatusDegraded,
		RetrievalStatusFailed,
	}
	for _, status := range valid {
		require.NoError(t, status.Validate())
	}
	require.ErrorIs(t, RetrievalStatus("bad").Validate(), domainerrors.ErrValidation)
}

func TestRetrievalStatusIsTerminal(t *testing.T) {
	require.False(t, RetrievalStatusRunning.IsTerminal())
	for _, status := range []RetrievalStatus{
		RetrievalStatusAvailable,
		RetrievalStatusEmpty,
		RetrievalStatusDegraded,
		RetrievalStatusFailed,
	} {
		require.True(t, status.IsTerminal())
	}
}

func TestCitationStatusValidate(t *testing.T) {
	require.NoError(t, CitationStatusValid.Validate())
	require.NoError(t, CitationStatusUnavailable.Validate())
	require.ErrorIs(t, CitationStatus("bad").Validate(), domainerrors.ErrValidation)
}

func TestSearchScopeValidate(t *testing.T) {
	require.NoError(t, SearchScopeSelected.Validate())
	require.NoError(t, SearchScopeAPIKeyBoundAll.Validate())
	require.ErrorIs(t, SearchScope("bad").Validate(), domainerrors.ErrValidation)
}

func TestNormalizeSearchScope(t *testing.T) {
	require.Equal(t, SearchScopeSelected, NormalizeSearchScope(""))
	require.Equal(t, SearchScopeSelected, NormalizeSearchScope(SearchScopeSelected))
	require.Equal(t, SearchScopeAPIKeyBoundAll, NormalizeSearchScope(SearchScopeAPIKeyBoundAll))
}
