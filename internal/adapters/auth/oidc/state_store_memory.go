package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// 编译期断言：MemoryStateStore 实现端口 auth.OIDCStateStore。
var _ authport.OIDCStateStore = (*MemoryStateStore)(nil)

// MemoryStateStore 是 OIDCStateStore 的进程内内存实现，供 standalone 无 Redis 模式使用。
//
// 语义与 RedisStateStore 对齐：state 一次性消费（mutex 临界区内的取出+删除等价于 GETDEL），
// 常量时间比较 browser nonce，不匹配时回写并重置为 full TTL（与 Redis 实现的 Set 行为一致）。
// TTL 由惰性过期 + 后台清理保证。重启清零（standalone 定位可接受；OIDC 登录窗口很短）。
type MemoryStateStore struct {
	mu       sync.Mutex
	entries  map[string]memoryStateEntry
	ttl      time.Duration
	stopOnce sync.Once
	stop     chan struct{}
}

type memoryStateEntry struct {
	payload  authport.OIDCStatePayload
	expireAt time.Time
}

// NewMemoryStateStore 构造一个内存 state store，ttl 为 state 有效期。
// 启动一个低频清理 goroutine 回收过期项，Close 停止它。
func NewMemoryStateStore(ttl time.Duration) *MemoryStateStore {
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	s := &MemoryStateStore{
		entries: make(map[string]memoryStateEntry),
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

func (s *MemoryStateStore) cleanupLoop() {
	ticker := time.NewTicker(s.ttl / 4)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.purgeExpired(time.Now())
		}
	}
}

func (s *MemoryStateStore) purgeExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if now.After(e.expireAt) {
			delete(s.entries, k)
		}
	}
}

// Close 停止后台清理 goroutine。可多次调用。
func (s *MemoryStateStore) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// Issue 生成一次性 state，把 payload 写入内存，返回 state。
func (s *MemoryStateStore) Issue(ctx context.Context, payload authport.OIDCStatePayload) (string, error) {
	state, err := randomHex(stateRandBytes)
	if err != nil {
		return "", fmt.Errorf("生成 oidc state 失败: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化 oidc state payload 失败: %w", err)
	}
	_ = data // payload 已存入 entry，data 仅做序列化校验
	now := time.Now()
	s.mu.Lock()
	s.entries[stateKeyPrefix+state] = memoryStateEntry{
		payload:  payload,
		expireAt: now.Add(s.ttl),
	}
	s.mu.Unlock()
	return state, nil
}

// Consume 在 mutex 临界区内原子取出并删除 state，常量时间比较 browser nonce。
// nonce 不匹配时回写 state 并重置为 full TTL（与 Redis 实现的 Set 行为一致）。
func (s *MemoryStateStore) Consume(ctx context.Context, state, browserNonce string) (*authport.OIDCStatePayload, error) {
	if len(state) > 128 || len(state) == 0 {
		return nil, ErrStateInvalid
	}
	key := stateKeyPrefix + state
	now := time.Now()
	s.mu.Lock()
	entry, ok := s.entries[key]
	if !ok || now.After(entry.expireAt) {
		delete(s.entries, key)
		s.mu.Unlock()
		return nil, ErrStateInvalid
	}
	delete(s.entries, key) // 一次性消费
	s.mu.Unlock()

	if subtle.ConstantTimeCompare([]byte(entry.payload.BrowserNonce), []byte(browserNonce)) != 1 {
		// nonce 不匹配：回写并重置为 full TTL（对齐 RedisStateStore 的 Set 行为）。
		s.mu.Lock()
		entry.expireAt = time.Now().Add(s.ttl)
		s.entries[key] = entry
		s.mu.Unlock()
		return nil, ErrStateInvalid
	}
	return &entry.payload, nil
}
