package web

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPageCacheCapacity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		maxPages int
		want     int
	}{
		{maxPages: 0, want: pageCacheMaxCapacity},
		{maxPages: -1, want: pageCacheMaxCapacity},
		{maxPages: 1, want: 1},
		{maxPages: 100, want: 100},
		{maxPages: pageCacheMaxCapacity, want: pageCacheMaxCapacity},
		{maxPages: pageCacheMaxCapacity + 1, want: pageCacheMaxCapacity},
		{maxPages: 10000, want: pageCacheMaxCapacity},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("max_pages=%d", tc.maxPages), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, NewPageCache(tc.maxPages).Capacity())
		})
	}
}

func TestPageCachePutGetAndEviction(t *testing.T) {
	t.Parallel()

	cache := NewPageCache(2)
	assert.Equal(t, 0, cache.Len())

	_, ok := cache.Get("https://example.com/a")
	assert.False(t, ok)

	cache.Put("https://example.com/a", fetchedPage{FinalURL: "https://example.com/a", Status: 200})
	cache.Put("https://example.com/b", fetchedPage{FinalURL: "https://example.com/b", Status: 200})
	assert.Equal(t, 2, cache.Len())

	// 命中 a 使其成为最近使用，b 变成最久未使用。
	page, ok := cache.Get("https://example.com/a")
	assert.True(t, ok)
	assert.Equal(t, 200, page.Status)

	// 逐出最久未使用的 b。
	cache.Put("https://example.com/c", fetchedPage{FinalURL: "https://example.com/c", Status: 404})
	assert.Equal(t, 2, cache.Len())
	_, ok = cache.Get("https://example.com/b")
	assert.False(t, ok, "容量超限时必须逐出最久未使用的条目")

	for _, key := range []string{"https://example.com/a", "https://example.com/c"} {
		_, ok = cache.Get(key)
		assert.True(t, ok, "key %s 应仍在缓存内", key)
	}
}

func TestPageCachePutExistingKeyRefreshesValue(t *testing.T) {
	t.Parallel()

	cache := NewPageCache(1)
	cache.Put("https://example.com/a", fetchedPage{Status: 500})
	cache.Put("https://example.com/a", fetchedPage{Status: 200})

	page, ok := cache.Get("https://example.com/a")
	assert.True(t, ok)
	assert.Equal(t, 200, page.Status)
	assert.Equal(t, 1, cache.Len())
}
