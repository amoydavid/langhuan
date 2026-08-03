package model

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestNewUserNormalizesEmail(t *testing.T) {
	user, err := NewUser("Alice@Example.COM", "Alice", "$argon2id$v=19$m=65536,t=3,p=2$abc")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("email = %q, want lowercased", user.Email)
	}
	if user.Nickname != "Alice" {
		t.Fatalf("nickname = %q", user.Nickname)
	}
	if user.PasswordHash == "" {
		t.Fatal("password hash should be stored")
	}
	if !user.CreatedAt.Equal(user.UpdatedAt) {
		t.Fatal("created_at and updated_at should match initially")
	}
}

func TestNewUserTrimsEmailAndNickname(t *testing.T) {
	user, err := NewUser("  alice@example.com  ", "  Alice  ", "$argon2id$hash")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("email = %q", user.Email)
	}
	if user.Nickname != "Alice" {
		t.Fatalf("nickname = %q", user.Nickname)
	}
}

func TestNewUserRejectsInvalidInput(t *testing.T) {
	validHash := "$argon2id$v=19$m=65536,t=3,p=2$abc"
	tests := []struct {
		name     string
		email    string
		nickname string
		hash     string
	}{
		{name: "empty email", email: "", nickname: "Alice", hash: validHash},
		{name: "invalid email", email: "not-an-email", nickname: "Alice", hash: validHash},
		{name: "email with display name", email: "Alice <alice@example.com>", nickname: "Alice", hash: validHash},
		{name: "empty nickname", email: "alice@example.com", nickname: "  ", hash: validHash},
		{name: "empty hash", email: "alice@example.com", nickname: "Alice", hash: ""},
		{name: "whitespace hash", email: "alice@example.com", nickname: "Alice", hash: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewUser(tt.email, tt.nickname, tt.hash)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}
