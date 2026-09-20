package web

import (
	"container/list"
	"sync"
)

// pageCacheMaxCapacity 是页缓存容量的硬上限（spec §5.1：容量 = min(max_pages, 500)）。
// 动机：crawl 发现阶段抓到一页后，Fetch 阶段不应二次抓取；但缓存不能按 max_pages
// 无界增长（既有 max_pages_ceiling 允许到 10000），故以 500 页封顶，超出按 LRU 逐出。
const pageCacheMaxCapacity = 500

// PageCache 是单次同步运行内的页缓存（不跨 run 复用）。
//
// 键是规范化后的 URL（页面身份，见 value.NormalizeWebSourceURI）；值是该次抓取的
// 统一内部结果 fetchedPage。命中即直接复用（含 NotModified 结果），逐出项回退为真实再抓。
// 并发安全：首版同 host 串行，但发现阶段可能跨 host 并发，故不依赖单线程假设。
type PageCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	order    *list.List
}

type pageCacheEntry struct {
	url  string
	page fetchedPage
}

// NewPageCache 按 max_pages 推导容量：max_pages <= 0 或超过 pageCacheMaxCapacity
// 时取 pageCacheMaxCapacity。
func NewPageCache(maxPages int) *PageCache {
	capacity := maxPages
	if capacity <= 0 || capacity > pageCacheMaxCapacity {
		capacity = pageCacheMaxCapacity
	}
	return &PageCache{
		capacity: capacity,
		entries:  make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

// Capacity 返回缓存容量上限。
func (c *PageCache) Capacity() int { return c.capacity }

// Len 返回当前缓存条目数（恒不超过 Capacity）。
func (c *PageCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Get 读取缓存并标记为最近使用。
func (c *PageCache) Get(normalizedURL string) (fetchedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[normalizedURL]
	if !ok {
		return fetchedPage{}, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*pageCacheEntry).page, true
}

// Put 写入缓存；容量超出时逐出最久未使用的条目。
func (c *PageCache) Put(normalizedURL string, page fetchedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[normalizedURL]; ok {
		element.Value.(*pageCacheEntry).page = page
		c.order.MoveToFront(element)
		return
	}
	c.entries[normalizedURL] = c.order.PushFront(&pageCacheEntry{url: normalizedURL, page: page})
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*pageCacheEntry).url)
	}
}
