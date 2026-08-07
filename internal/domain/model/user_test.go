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

func TestNewProvisionalUserCreatesPasswordlessAccount(t *testing.T) {
	user, err := NewProvisionalUser("ada@example.com", "Ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Email != "ada@example.com" {
		t.Fatalf("email = %q", user.Email)
	}
	if user.Nickname != "Ada" {
		t.Fatalf("nickname = %q", user.Nickname)
	}
	if user.PasswordHash != "" {
		t.Fatalf("provisional user password_hash should be empty, got %q", user.PasswordHash)
	}
	if user.HasPassword() {
		t.Fatal("provisional user should not have a password")
	}
}

func TestNewProvisionalUserRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		nickname string
	}{
		{name: "empty email", email: "", nickname: "Ada"},
		{name: "invalid email", email: "not-an-email", nickname: "Ada"},
		{name: "empty nickname", email: "ada@example.com", nickname: "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProvisionalUser(tt.email, tt.nickname)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestUserHasPassword(t *testing.T) {
	pwUser, err := NewUser("alice@example.com", "Alice", "$argon2id$hash")
	if err != nil {
		t.Fatal(err)
	}
	if !pwUser.HasPassword() {
		t.Fatal("user created via NewUser should report HasPassword=true")
	}

	provUser, err := NewProvisionalUser("bob@example.com", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if provUser.HasPassword() {
		t.Fatal("provisional user should report HasPassword=false")
	}
}
