package db

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestAESGCMCredentialCipherRoundTripUsesRandomNonce(t *testing.T) {
	t.Parallel()

	cipher, err := NewAESGCMCredentialCipher(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	providerID := uuid.New()
	plaintext := []byte(`{"api_key":"secret"}`)
	one, err := cipher.Encrypt(providerID, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	two, err := cipher.Encrypt(providerID, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(one, two) {
		t.Fatal("ciphertexts must differ because each encryption uses a random nonce")
	}
	if len(one) <= 13 || one[0] != 1 {
		t.Fatalf("ciphertext format = %x", one)
	}
	got, err := cipher.Decrypt(providerID, one)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext = %q, want %q", got, plaintext)
	}
}

func TestAESGCMCredentialCipherBindsCiphertextToProvider(t *testing.T) {
	t.Parallel()

	cipher, err := NewAESGCMCredentialCipher(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	providerA, providerB := uuid.New(), uuid.New()
	sealed, err := cipher.Encrypt(providerA, []byte(`{"api_key":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.Decrypt(providerB, sealed); !errors.Is(err, domainerrors.ErrCredentialDecryption) {
		t.Fatalf("cross-provider decrypt error = %v", err)
	}
}

func TestAESGCMCredentialCipherRejectsTamperingVersionAndWrongKey(t *testing.T) {
	t.Parallel()

	providerID := uuid.New()
	cipher, err := NewAESGCMCredentialCipher(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cipher.Encrypt(providerID, []byte(`{"api_key":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	wrongVersion := append([]byte(nil), sealed...)
	wrongVersion[0] = 2
	wrongKey, err := NewAESGCMCredentialCipher(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}

	for name, decrypt := range map[string]func() error{
		"tampered":      func() error { _, err := cipher.Decrypt(providerID, tampered); return err },
		"wrong version": func() error { _, err := cipher.Decrypt(providerID, wrongVersion); return err },
		"too short":     func() error { _, err := cipher.Decrypt(providerID, []byte{1}); return err },
		"wrong key":     func() error { _, err := wrongKey.Decrypt(providerID, sealed); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := decrypt(); !errors.Is(err, domainerrors.ErrCredentialDecryption) {
				t.Fatalf("error = %v, want ErrCredentialDecryption", err)
			}
		})
	}
}

func TestAESGCMCredentialCipherRequires32ByteKeyAndProviderID(t *testing.T) {
	t.Parallel()

	if _, err := NewAESGCMCredentialCipher([]byte("short")); err == nil {
		t.Fatal("expected key length error")
	}
	cipher, err := NewAESGCMCredentialCipher(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.Encrypt(uuid.Nil, []byte(`{}`)); err == nil {
		t.Fatal("expected nil provider id error")
	}
}
