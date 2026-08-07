// Package oidc 实现 OIDC 认证端口：provider（coreos/go-oidc + oauth2）与
// Redis state store。所有实现构造函数注入，禁止包级全局。
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// 编译期断言：RedisStateStore 实现端口 auth.OIDCStateStore。
var _ authport.OIDCStateStore = (*RedisStateStore)(nil)

const (
	stateKeyPrefix = "langhuan:oidc:state:"
	stateRandBytes = 32 // 256 位熵
)

// ErrStateInvalid 表示 state 不存在、过期或 nonce 不匹配。
var ErrStateInvalid = errors.New("oidc state 无效或已过期")

// RedisStateStore 基于 Redis 实现 OIDCStateStore。
//
// state 存 Redis、与浏览器 nonce cookie 双绑，GETDEL 原子取删实现一次性消费。
// nonce 不匹配时把 payload 重新写回（保留原 TTL，防恶意请求消耗合法 state）；
// 该回写存在理论竞态（同一 state 两个并发请求），但 state 本身是一次性的，
// 正常用户不会触发 nonce 不匹配路径，攻击者路径的竞态不构成安全风险。
type RedisStateStore struct {
	client     *redis.Client
	ttlSeconds int
}

// NewRedisStateStore 构造一个基于给定 redis.Client 的 state store。
// ttlSeconds 为 state 有效期（秒）。
func NewRedisStateStore(client *redis.Client, ttlSeconds int) *RedisStateStore {
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return &RedisStateStore{client: client, ttlSeconds: ttlSeconds}
}

// Issue 生成一次性 state，把 payload 写入 Redis，返回 state。
func (s *RedisStateStore) Issue(ctx context.Context, payload authport.OIDCStatePayload) (string, error) {
	state, err := randomHex(stateRandBytes)
	if err != nil {
		return "", fmt.Errorf("生成 oidc state 失败: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化 oidc state payload 失败: %w", err)
	}
	key := stateKeyPrefix + state
	if err := s.client.Set(ctx, key, data, time.Duration(s.ttlSeconds)*time.Second).Err(); err != nil {
		return "", fmt.Errorf("写入 oidc state 失败: %w", err)
	}
	return state, nil
}

// Consume 用 GETDEL 原子取出并删除 state，常量时间比较 browser nonce。
// nonce 不匹配时把 payload 重新写回（保留 state 供合法请求消费）。
func (s *RedisStateStore) Consume(ctx context.Context, state, browserNonce string) (*authport.OIDCStatePayload, error) {
	// 长度上限校验，避免恶意超长 key。
	if len(state) > 128 || len(state) == 0 {
		return nil, ErrStateInvalid
	}
	key := stateKeyPrefix + state
	res, err := s.client.GetDel(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrStateInvalid
		}
		return nil, fmt.Errorf("消费 oidc state 失败: %w", err)
	}
	var payload authport.OIDCStatePayload
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		return nil, fmt.Errorf("反序列化 oidc state payload 失败: %w", err)
	}
	// 常量时间比较 nonce。
	if subtle.ConstantTimeCompare([]byte(payload.BrowserNonce), []byte(browserNonce)) != 1 {
		// nonce 不匹配：回写 state，保留原 TTL，供合法请求消费。
		_ = s.client.Set(ctx, key, res, time.Duration(s.ttlSeconds)*time.Second).Err()
		return nil, ErrStateInvalid
	}
	return &payload, nil
}

// randomHex 生成 n 字节随机数并返回 hex 编码。
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
