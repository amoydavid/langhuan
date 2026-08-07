package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestNewExternalIdentity(t *testing.T) {
	userID := uuid.New()
	got, err := NewExternalIdentity(userID, "https://sso.example.com", "sub-123", "ada@example.com", true, `{"sub":"sub-123"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatal("ID should be generated")
	}
	if got.UserID != userID {
		t.Fatalf("UserID = %v, want %v", got.UserID, userID)
	}
	if got.Issuer != "https://sso.example.com" {
		t.Fatalf("Issuer = %q", got.Issuer)
	}
	if got.Subject != "sub-123" {
		t.Fatalf("Subject = %q", got.Subject)
	}
	if got.Email != "ada@example.com" {
		t.Fatalf("Email = %q", got.Email)
	}
	if !got.EmailVerified {
		t.Fatal("EmailVerified should be true")
	}
	if !got.CreatedAt.Equal(got.UpdatedAt) {
		t.Fatal("CreatedAt and UpdatedAt should match initially")
	}
}

func TestNewExternalIdentityRejectsInvalidInput(t *testing.T) {
	userID := uuid.New()
	tests := []struct {
		name          string
		userID        uuid.UUID
		issuer        string
		subject       string
		email         string
		emailVerified bool
		rawProfile    string
	}{
		{name: "nil user id", userID: uuid.Nil, issuer: "https://sso.example.com", subject: "sub-1", email: "a@b.com", emailVerified: true, rawProfile: "{}"},
		{name: "empty issuer", userID: userID, issuer: "  ", subject: "sub-1", email: "a@b.com", emailVerified: true, rawProfile: "{}"},
		{name: "empty subject", userID: userID, issuer: "https://sso.example.com", subject: "", email: "a@b.com", emailVerified: true, rawProfile: "{}"},
		{name: "empty email", userID: userID, issuer: "https://sso.example.com", subject: "sub-1", email: "  ", emailVerified: true, rawProfile: "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewExternalIdentity(tt.userID, tt.issuer, tt.subject, tt.email, tt.emailVerified, tt.rawProfile)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}
