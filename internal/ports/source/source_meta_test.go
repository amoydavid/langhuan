package source

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceMetaToMapOmitsZeroFields(t *testing.T) {
	t.Parallel()

	fetchedAt := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	got := SourceMeta{
		ETag:           `"abc"`,
		LastModified:   "Tue, 01 Sep 2026 00:00:00 GMT",
		LastEngine:     "http",
		LastFetchAt:    fetchedAt,
		LastHTTPStatus: 304,
		FinalURL:       "https://example.com/redirected",
	}.ToMap()

	assert.Equal(t, map[string]any{
		SourceMetaETag:           `"abc"`,
		SourceMetaLastModified:   "Tue, 01 Sep 2026 00:00:00 GMT",
		SourceMetaLastEngine:     "http",
		SourceMetaLastFetchAt:    "2026-09-16T08:00:00Z",
		SourceMetaLastHTTPStatus: 304,
		SourceMetaFinalURL:       "https://example.com/redirected",
	}, got)

	assert.Empty(t, SourceMeta{}.ToMap())
}

func TestSourceMetaRoundTrip(t *testing.T) {
	t.Parallel()

	original := SourceMeta{
		ETag:           `W/"v1"`,
		LastModified:   "Tue, 01 Sep 2026 00:00:00 GMT",
		LastEngine:     "http",
		LastFetchAt:    time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC),
		LastHTTPStatus: 200,
		FinalURL:       "https://example.com/a",
	}
	// 模拟落库后从 jsonb 读回：先经过 JSON 编解码，使时间变成字符串、数字变成 float64。
	encoded, err := json.Marshal(original.ToMap())
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, original, SourceMetaFromMap(decoded))
}

func TestSourceMetaFromMapToleratesMissingAndWrongTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, SourceMeta{}, SourceMetaFromMap(nil))
	assert.Equal(t, SourceMeta{}, SourceMetaFromMap(map[string]any{}))

	got := SourceMetaFromMap(map[string]any{
		SourceMetaETag:           42,
		SourceMetaLastFetchAt:    "not-a-time",
		SourceMetaLastHTTPStatus: "200",
	})
	assert.Equal(t, SourceMeta{}, got)

	withTime := SourceMetaFromMap(map[string]any{
		SourceMetaLastFetchAt: time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC),
	})
	assert.Equal(t, time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC), withTime.LastFetchAt)
}
