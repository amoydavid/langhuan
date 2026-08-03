package embedding

import "github.com/google/uuid"

// CredentialCipher 负责按 Provider 身份加密和解密完整凭证 JSON。
type CredentialCipher interface {
	Encrypt(providerID uuid.UUID, plaintext []byte) ([]byte, error)
	Decrypt(providerID uuid.UUID, ciphertext []byte) ([]byte, error)
}
