package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSessionGeneratesIDAndExpiry(t *testing.T) {
	before := time.Now().UTC()
	lifetime := 7 * 24 * time.Hour
	session, err := NewSession(uuid.New(), lifetime, "Mozilla/5.0", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	if session.ID == uuid.Nil {
		t.Fatal("session ID should be a non-nil random UUID")
	}
	if session.UserID == uuid.Nil {
		t.Fatal("user id should be propagated")
	}
	if session.CreatedAt.Before(before) {
		t.Fatal("created_at should be now or later")
	}
	if !session.CreatedAt.Equal(session.LastSeenAt) {
		t.Fatalf("last_seen_at should equal created_at initially; created=%s last_seen=%s", session.CreatedAt, session.LastSeenAt)
	}
	wantExpiry := session.CreatedAt.Add(lifetime)
	if !session.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expires_at = %s, want %s", session.ExpiresAt, wantExpiry)
	}
	if session.UserAgent != "Mozilla/5.0" {
		t.Fatalf("user_agent = %q", session.UserAgent)
	}
	if session.IPAddr != "127.0.0.1" {
		t.Fatalf("ip_addr = %q", session.IPAddr)
	}
	if session.RevokedAt != nil {
		t.Fatal("revoked_at should be nil for a new session")
	}
}

func TestNewSessionRejectsInvalidInput(t *testing.T) {
	lifetime := time.Hour
	if _, err := NewSession(uuid.Nil, lifetime, "ua", "1.1.1.1"); err == nil {
		t.Fatal("expected error for nil user id")
	}
	if _, err := NewSession(uuid.New(), 0, "ua", "1.1.1.1"); err == nil {
		t.Fatal("expected error for zero lifetime")
	}
	if _, err := NewSession(uuid.New(), -time.Second, "ua", "1.1.1.1"); err == nil {
		t.Fatal("expected error for negative lifetime")
	}
}
