package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateAPIKeyMaterial(t *testing.T) {
	material, err := GenerateAPIKeyMaterial(bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32)))
	require.NoError(t, err)
	require.Regexp(t, `^lhk_[A-Za-z0-9_-]{43}$`, material.Plaintext)
	require.Len(t, material.Plaintext, 47)
	require.Equal(t, material.Plaintext[:12], material.Prefix)
	// hash 是完整明文的 SHA-256 lowercase hex。
	sum := sha256.Sum256([]byte(material.Plaintext))
	require.Equal(t, hex.EncodeToString(sum[:]), material.Hash)
	require.Regexp(t, `^[0-9a-f]{64}$`, material.Hash)
}

func TestGenerateAPIKeyMaterialDecodesTo32RandomBytes(t *testing.T) {
	material, err := GenerateAPIKeyMaterial(bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32)))
	require.NoError(t, err)
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(material.Plaintext, "lhk_"))
	require.NoError(t, err)
	require.Len(t, secret, 32)
}

func TestGenerateAPIKeyMaterialFailsOnShortRandom(t *testing.T) {
	_, err := GenerateAPIKeyMaterial(bytes.NewReader([]byte{0x01, 0x02}))
	require.Error(t, err)
}

func TestGenerateAPIKeyMaterialRejectsNilRandom(t *testing.T) {
	_, err := GenerateAPIKeyMaterial(nil)
	require.Error(t, err)
}

func TestHashAPIKeyRejectsInvalidFormat(t *testing.T) {
	cases := []string{"", "lhk_short", "notprefixed_" + strings.Repeat("x", 43), "lhk_" + strings.Repeat("x", 42)}
	for _, plaintext := range cases {
		_, err := HashAPIKey(plaintext)
		require.Error(t, err, plaintext)
	}
}

func TestHashAPIKeyIsDeterministic(t *testing.T) {
	plaintext := "lhk_" + strings.Repeat("x", 43)
	h1, err := HashAPIKey(plaintext)
	require.NoError(t, err)
	h2, err := HashAPIKey(plaintext)
	require.NoError(t, err)
	require.Equal(t, h1, h2)
	// 不同明文产生不同 hash。
	other, err := HashAPIKey("lhk_" + strings.Repeat("y", 43))
	require.NoError(t, err)
	require.NotEqual(t, h1, other)
}

func TestGenerateAPIKeyMaterialDoesNotPrintPlaintextOnError(t *testing.T) {
	// 失败路径不应包含明文；这里只验证错误信息不含明文格式。
	_, err := GenerateAPIKeyMaterial(bytes.NewReader([]byte{0x01}))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "lhk_")
}
