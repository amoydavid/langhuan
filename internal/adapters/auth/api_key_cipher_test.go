package auth

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

var apikeyPlaintextSample = "lhk_" + strings.Repeat("x", 43)

func TestAPIKeyCipherRoundTrip(t *testing.T) {
	cipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	workspaceID, keyID := uuid.New(), uuid.New()

	ciphertext, err := cipher.Encrypt(workspaceID, keyID, []byte(apikeyPlaintextSample))
	require.NoError(t, err)

	plaintext, err := cipher.Decrypt(workspaceID, keyID, ciphertext)
	require.NoError(t, err)
	require.Equal(t, apikeyPlaintextSample, string(plaintext))
}

func TestAPIKeyCipherBindsWorkspaceAndKeyID(t *testing.T) {
	cipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	workspaceID, keyID := uuid.New(), uuid.New()
	ciphertext, err := cipher.Encrypt(workspaceID, keyID, []byte(apikeyPlaintextSample))
	require.NoError(t, err)

	if _, err := cipher.Decrypt(uuid.New(), keyID, ciphertext); err == nil {
		t.Fatal("decrypt with wrong workspace should fail")
	}
	if _, err := cipher.Decrypt(workspaceID, uuid.New(), ciphertext); err == nil {
		t.Fatal("decrypt with wrong key id should fail")
	}
}

func TestAPIKeyCipherRejectsWrongMasterKey(t *testing.T) {
	encryptCipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	decryptCipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x22}, 32), rand.Reader)
	require.NoError(t, err)
	workspaceID, keyID := uuid.New(), uuid.New()
	ciphertext, err := encryptCipher.Encrypt(workspaceID, keyID, []byte(apikeyPlaintextSample))
	require.NoError(t, err)
	_, err = decryptCipher.Decrypt(workspaceID, keyID, ciphertext)
	require.ErrorIs(t, err, domainerrors.ErrAPIKeySecretUnavailable)
}

func TestAPIKeyCipherRejectsTamperedCiphertext(t *testing.T) {
	cipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	workspaceID, keyID := uuid.New(), uuid.New()
	ciphertext, err := cipher.Encrypt(workspaceID, keyID, []byte(apikeyPlaintextSample))
	require.NoError(t, err)

	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xff // flip last byte
	_, err = cipher.Decrypt(workspaceID, keyID, tampered)
	require.ErrorIs(t, err, domainerrors.ErrAPIKeySecretUnavailable)
}

func TestAPIKeyCipherEachEncryptionUsesRandomNonce(t *testing.T) {
	cipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	workspaceID, keyID := uuid.New(), uuid.New()
	ct1, err := cipher.Encrypt(workspaceID, keyID, []byte(apikeyPlaintextSample))
	require.NoError(t, err)
	ct2, err := cipher.Encrypt(workspaceID, keyID, []byte(apikeyPlaintextSample))
	require.NoError(t, err)
	// 相同明文产生不同密文（随机 nonce）。
	require.NotEqual(t, ct1, ct2)
}

func TestAPIKeyCipherRejectsInvalidMasterKeyLength(t *testing.T) {
	_, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 31), rand.Reader)
	require.Error(t, err)
}

func TestAPIKeyCipherRejectsNilIDs(t *testing.T) {
	cipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	_, err = cipher.Encrypt(uuid.Nil, uuid.New(), []byte(apikeyPlaintextSample))
	require.Error(t, err)
	_, err = cipher.Decrypt(uuid.New(), uuid.Nil, []byte{1, 2, 3})
	require.Error(t, err)
}

func TestAPIKeyCipherRejectsMalformedCiphertext(t *testing.T) {
	cipher, err := NewAPIKeyCipher(bytes.Repeat([]byte{0x11}, 32), rand.Reader)
	require.NoError(t, err)
	workspaceID, keyID := uuid.New(), uuid.New()
	// 错误版本字节。
	_, err = cipher.Decrypt(workspaceID, keyID, []byte{0x02, 0x01, 0x02, 0x03})
	require.ErrorIs(t, err, domainerrors.ErrAPIKeySecretUnavailable)
	// 太短。
	_, err = cipher.Decrypt(workspaceID, keyID, []byte{0x01})
	require.ErrorIs(t, err, domainerrors.ErrAPIKeySecretUnavailable)
}
