package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiscoverURLsPassesThroughNormalizedList(t *testing.T) {
	t.Parallel()

	urls, warnings := DiscoverURLs([]string{
		"https://example.com/a",
		"http://example.com/b",
		"https://example.com/docs/",
	})

	assert.Equal(t, []string{
		"https://example.com/a",
		"http://example.com/b",
		"https://example.com/docs/",
	}, urls)
	assert.Empty(t, warnings)
}

func TestDiscoverURLsKeepsWriteBoundaryNormalization(t *testing.T) {
	t.Parallel()

	urls, warnings := DiscoverURLs([]string{
		"https://example.com/a?utm_source=news&id=1#frag",
		"HTTPS://EXAMPLE.com:443/B",
		"  https://example.com/c  ",
	})

	assert.Equal(t, []string{
		"https://example.com/a?id=1",
		"https://example.com/B",
		"https://example.com/c",
	}, urls)
	assert.Empty(t, warnings)
}

func TestDiscoverURLsDropsInvalidEntriesWithWarning(t *testing.T) {
	t.Parallel()

	urls, warnings := DiscoverURLs([]string{
		"https://example.com/a",
		"",
		"   ",
		"/relative",
		"javascript:void(0)",
		"ftp://example.com/f",
		"https://user:pass@example.com/secret",
		"https://example.com/b",
	})

	assert.Equal(t, []string{"https://example.com/a", "https://example.com/b"}, urls)
	assert.Equal(t, []string{
		WarningInvalidURL,
		WarningInvalidURL,
		WarningInvalidURL,
		WarningInvalidURL,
		WarningInvalidURL,
		WarningInvalidURL,
	}, warnings)
}

func TestDiscoverURLsDeduplicatesKeepingFirstOccurrence(t *testing.T) {
	t.Parallel()

	urls, warnings := DiscoverURLs([]string{
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/a?utm_source=x",
		"https://example.com/b#section",
	})

	assert.Equal(t, []string{"https://example.com/a", "https://example.com/b"}, urls)
	assert.Empty(t, warnings)
}

func TestDiscoverURLsWithEmptyList(t *testing.T) {
	t.Parallel()

	urls, warnings := DiscoverURLs(nil)
	assert.Empty(t, urls)
	assert.Empty(t, warnings)
}
