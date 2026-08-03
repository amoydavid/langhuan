// Package auth 实现 Workspace 认证相关的适配器，包括 API Key 可恢复明文的
// HKDF + AES-256-GCM 加密边界。它与模型 Provider 凭证加密器严格分离。
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

const (
	// apiKeyCipherVersion 是密文 envelope 的格式版本字节。
	apiKeyCipherVersion byte = 1
	// apiKeyHKDFInfo 绑定 HKDF 派生用途，与 Provider cipher 域隔离。
	apiKeyHKDFInfo      = "langhuan/workspace-api-key/v1"
	apiKeyAADPrefix     = "workspace-api-key:v1"
	apiKeyDerivedKeyLen = 32
)

// APIKeyCipher 用 HKDF-SHA-256 从主密钥派生独立子密钥，再用 AES-256-GCM
// 加密可恢复明文。AAD 绑定 (workspaceID, apiKeyID)，跨行/跨 Workspace 复制
// 密文必须解密失败。
type APIKeyCipher struct {
	master []byte
	aead   cipher.AEAD
	random io.Reader
}

// NewAPIKeyCipher 从 Base64 解码后的 32-byte 主密钥构造 cipher。
// 主密钥与 Provider 凭证主密钥复用 credentials.encryption_key，但通过 HKDF
// 派生独立子密钥，互不影响。random 为 nil 时使用 crypto/rand.Reader。
func NewAPIKeyCipher(master []byte, random io.Reader) (*APIKeyCipher, error) {
	if len(master) != 32 {
		return nil, fmt.Errorf("API Key 主密钥必须为 32 字节")
	}
	derived, err := deriveAPIKeyPurposeKey(master)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("初始化 API Key 加密算法失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 API Key GCM 失败: %w", err)
	}
	if random == nil {
		random = rand.Reader
	}
	return &APIKeyCipher{master: master, aead: aead, random: random}, nil
}

// Encrypt 用绑定 (workspaceID, apiKeyID) 的 AAD 加密明文，返回版本化密文。
func (c *APIKeyCipher) Encrypt(workspaceID, apiKeyID uuid.UUID, plaintext []byte) ([]byte, error) {
	if workspaceID == uuid.Nil || apiKeyID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/api_key_id 不能为空", domainerrors.ErrValidation)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return nil, fmt.Errorf("生成 API Key 加密 nonce 失败: %w", err)
	}
	aad := apiKeyAAD(workspaceID, apiKeyID)
	result := make([]byte, 1+len(nonce), 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	result[0] = apiKeyCipherVersion
	copy(result[1:], nonce)
	result = c.aead.Seal(result, nonce, plaintext, aad)
	return result, nil
}

// Decrypt 用相同 AAD 解密密文；版本不符、AAD 篡改或密文损坏均返回错误。
func (c *APIKeyCipher) Decrypt(workspaceID, apiKeyID uuid.UUID, ciphertext []byte) ([]byte, error) {
	if workspaceID == uuid.Nil || apiKeyID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/api_key_id 不能为空", domainerrors.ErrValidation)
	}
	minimumLength := 1 + c.aead.NonceSize() + c.aead.Overhead()
	if len(ciphertext) < minimumLength || ciphertext[0] != apiKeyCipherVersion {
		return nil, fmt.Errorf("%w: 密文格式无效", domainerrors.ErrAPIKeySecretUnavailable)
	}
	nonce := ciphertext[1 : 1+c.aead.NonceSize()]
	sealed := ciphertext[1+c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, sealed, apiKeyAAD(workspaceID, apiKeyID))
	if err != nil {
		return nil, fmt.Errorf("%w: 密文认证失败", domainerrors.ErrAPIKeySecretUnavailable)
	}
	return plaintext, nil
}

func deriveAPIKeyPurposeKey(master []byte) ([]byte, error) {
	derived := make([]byte, apiKeyDerivedKeyLen)
	reader := hkdf.New(sha256.New, master, nil, []byte(apiKeyHKDFInfo))
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil, fmt.Errorf("派生 API Key 子密钥失败: %w", err)
	}
	return derived, nil
}

func apiKeyAAD(workspaceID, apiKeyID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s:%s:%s", apiKeyAADPrefix, workspaceID, apiKeyID))
}
