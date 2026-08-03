package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

const (
	// apiKeyPlaintextPrefix 固定前缀，便于识别、脱敏和 secret scanner 发现。
	apiKeyPlaintextPrefix = "lhk_"
	// apiKeySecretBytes 是 crypto/rand 生成的随机字节数，提供 256 bit 熵。
	apiKeySecretBytes = 32
	// apiKeyPlaintextLen 是完整明文长度：4 字符前缀 + 43 字符 Base64URL。
	apiKeyPlaintextLen = 47
	// apiKeyPrefixDisplayLen 是 token_prefix 展示长度（前缀 + 前 8 个字符）。
	apiKeyPrefixDisplayLen = 12
)

// APIKeyMaterial 持有一次生成的 API Key 明文、hash 与展示前缀。
//
// Plaintext 只在创建/Reveal 响应中短暂出现；Hash 永久存索引；Prefix 用于
// 列表展示。三者由同一明文派生，互不包含可逆信息。
type APIKeyMaterial struct {
	Plaintext string
	Hash      string
	Prefix    string
}

// GenerateAPIKeyMaterial 使用注入的随机源生成固定格式的 API Key 材料。
//
// 失败（例如随机源不可用）时返回错误且不返回部分材料。random 必须能提供
// 至少 32 字节熵，生产路径使用 crypto/rand.Reader。
func GenerateAPIKeyMaterial(random io.Reader) (APIKeyMaterial, error) {
	if random == nil {
		return APIKeyMaterial{}, fmt.Errorf("生成 API Key 失败: 随机源不能为空")
	}
	secret := make([]byte, apiKeySecretBytes)
	if _, err := io.ReadFull(random, secret); err != nil {
		return APIKeyMaterial{}, fmt.Errorf("生成 API Key 随机数失败: %w", err)
	}
	plaintext := apiKeyPlaintextPrefix + base64.RawURLEncoding.EncodeToString(secret)
	return APIKeyMaterial{
		Plaintext: plaintext,
		Hash:      hashAPIKey(plaintext),
		Prefix:    plaintext[:apiKeyPrefixDisplayLen],
	}, nil
}

// HashAPIKey 计算完整明文的 SHA-256 lowercase hex。
//
// 仅用于把请求 Bearer 映射到索引；格式错误时返回错误，调用方据此拒绝。
func HashAPIKey(plaintext string) (string, error) {
	if err := ValidateAPIKeyPlaintext(plaintext); err != nil {
		return "", err
	}
	return hashAPIKey(plaintext), nil
}

// ValidateAPIKeyPlaintext 校验明文是否为固定格式 lhk_ + 43 字符 Base64URL。
func ValidateAPIKeyPlaintext(plaintext string) error {
	if len(plaintext) != apiKeyPlaintextLen {
		return fmt.Errorf("%w: API Key 长度不合法", domainerrors.ErrAPIKeyInvalidFormat)
	}
	if plaintext[:len(apiKeyPlaintextPrefix)] != apiKeyPlaintextPrefix {
		return fmt.Errorf("%w: API Key 缺少前缀", domainerrors.ErrAPIKeyInvalidFormat)
	}
	return nil
}

func hashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
