package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

const credentialCipherVersion byte = 1

type aesGCMCredentialCipher struct {
	aead cipher.AEAD
}

// NewAESGCMCredentialCipher 使用恰好 32 字节的 key 构造 AES-256-GCM cipher。
func NewAESGCMCredentialCipher(key []byte) (embeddingport.CredentialCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("credentials encryption key 必须为 32 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化凭证加密算法失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化凭证 GCM 失败: %w", err)
	}
	return &aesGCMCredentialCipher{aead: aead}, nil
}

func (c *aesGCMCredentialCipher) Encrypt(providerID uuid.UUID, plaintext []byte) ([]byte, error) {
	if providerID == uuid.Nil {
		return nil, fmt.Errorf("%w: provider_id 不能为空", domainerrors.ErrValidation)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成凭证加密 nonce 失败: %w", err)
	}

	result := make([]byte, 1+len(nonce), 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	result[0] = credentialCipherVersion
	copy(result[1:], nonce)
	result = c.aead.Seal(result, nonce, plaintext, credentialAAD(providerID))
	return result, nil
}

func (c *aesGCMCredentialCipher) Decrypt(providerID uuid.UUID, ciphertext []byte) ([]byte, error) {
	if providerID == uuid.Nil {
		return nil, fmt.Errorf("%w: provider_id 不能为空", domainerrors.ErrCredentialDecryption)
	}
	minimumLength := 1 + c.aead.NonceSize() + c.aead.Overhead()
	if len(ciphertext) < minimumLength || ciphertext[0] != credentialCipherVersion {
		return nil, fmt.Errorf("%w: 密文格式无效", domainerrors.ErrCredentialDecryption)
	}
	nonce := ciphertext[1 : 1+c.aead.NonceSize()]
	sealed := ciphertext[1+c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, sealed, credentialAAD(providerID))
	if err != nil {
		return nil, fmt.Errorf("%w: 密文认证失败", domainerrors.ErrCredentialDecryption)
	}
	return plaintext, nil
}

func credentialAAD(providerID uuid.UUID) []byte {
	return []byte("model-provider:" + providerID.String())
}

// sourceConnectionCredentialCipher 使用与 model provider 相同的 AES-256-GCM key，
// 但 AAD 前缀为 "source-connection:"，使两类凭证密文物理隔离（同一密文不可跨用途解密）。
type sourceConnectionCredentialCipher struct {
	aead cipher.AEAD
}

// NewSourceConnectionCredentialCipher 构造用于 workspace_source_connections 凭证的 cipher。
func NewSourceConnectionCredentialCipher(key []byte) (*sourceConnectionCredentialCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("credentials encryption key 必须为 32 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化凭证加密算法失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化凭证 GCM 失败: %w", err)
	}
	return &sourceConnectionCredentialCipher{aead: aead}, nil
}

func (c *sourceConnectionCredentialCipher) Encrypt(connectionID uuid.UUID, plaintext []byte) ([]byte, error) {
	if connectionID == uuid.Nil {
		return nil, fmt.Errorf("%w: connection_id 不能为空", domainerrors.ErrValidation)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成凭证加密 nonce 失败: %w", err)
	}
	result := make([]byte, 1+len(nonce), 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	result[0] = credentialCipherVersion
	copy(result[1:], nonce)
	result = c.aead.Seal(result, nonce, plaintext, sourceConnectionAAD(connectionID))
	return result, nil
}

func (c *sourceConnectionCredentialCipher) Decrypt(connectionID uuid.UUID, ciphertext []byte) ([]byte, error) {
	if connectionID == uuid.Nil {
		return nil, fmt.Errorf("%w: connection_id 不能为空", domainerrors.ErrCredentialDecryption)
	}
	minimumLength := 1 + c.aead.NonceSize() + c.aead.Overhead()
	if len(ciphertext) < minimumLength || ciphertext[0] != credentialCipherVersion {
		return nil, fmt.Errorf("%w: 密文格式无效", domainerrors.ErrCredentialDecryption)
	}
	nonce := ciphertext[1 : 1+c.aead.NonceSize()]
	sealed := ciphertext[1+c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, sealed, sourceConnectionAAD(connectionID))
	if err != nil {
		return nil, fmt.Errorf("%w: 密文认证失败", domainerrors.ErrCredentialDecryption)
	}
	return plaintext, nil
}

func sourceConnectionAAD(connectionID uuid.UUID) []byte {
	return []byte("source-connection:" + connectionID.String())
}
