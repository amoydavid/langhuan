package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// canonicalSearchQuery 规范化检索 query，固定为去首尾空白。
// 该规范化版本用于计算 query_hash 和回放验证，不保存原始 query。
func canonicalSearchQuery(raw string) string {
	return strings.TrimSpace(raw)
}

// searchQueryHash 对规范化 query 按 UTF-8 字节计算 SHA-256，输出 "sha256:v1:<hex>"。
// hash 只用于验证回放请求是否是原问题，不能用于还原 query，也不能作为授权凭证。
func searchQueryHash(raw string) string {
	sum := sha256.Sum256([]byte(canonicalSearchQuery(raw)))
	return "sha256:v1:" + hex.EncodeToString(sum[:])
}
