package value

import (
	"testing"

	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestValidateInsightsWindowDays(t *testing.T) {
	require.NoError(t, ValidateInsightsWindowDays(1))
	require.NoError(t, ValidateInsightsWindowDays(14))
	require.NoError(t, ValidateInsightsWindowDays(InsightsWindowMaxDays))
	for _, days := range []int{0, -1, InsightsWindowMaxDays + 1} {
		require.ErrorIs(t, ValidateInsightsWindowDays(days), domainerrors.ErrValidation, "days=%d", days)
	}
}

func TestValidateInsightsListLimit(t *testing.T) {
	require.NoError(t, ValidateInsightsListLimit(1))
	require.NoError(t, ValidateInsightsListLimit(InsightsListMaxLimit))
	for _, limit := range []int{0, -5, InsightsListMaxLimit + 1} {
		require.ErrorIs(t, ValidateInsightsListLimit(limit), domainerrors.ErrValidation, "limit=%d", limit)
	}
}

func TestValidateInsightsActivityLimit(t *testing.T) {
	require.NoError(t, ValidateInsightsActivityLimit(1))
	require.NoError(t, ValidateInsightsActivityLimit(InsightsActivityMaxLimit))
	for _, limit := range []int{0, InsightsActivityMaxLimit + 1} {
		require.ErrorIs(t, ValidateInsightsActivityLimit(limit), domainerrors.ErrValidation, "limit=%d", limit)
	}
}
