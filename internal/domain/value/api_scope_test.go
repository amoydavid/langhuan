package value

import (
	"slices"
	"testing"
	"time"
)

func TestAllAPIScopesCoversExactSet(t *testing.T) {
	want := []APIScope{
		ScopeKnowledgeBasesWrite,
		ScopeDocumentsRead,
		ScopeDocumentsWrite,
		ScopeSearchRead,
	}
	got := AllAPIScopes()
	if !slices.Equal(got, want) {
		t.Fatalf("AllAPIScopes() = %v, want %v", got, want)
	}
}

func TestIsValidAPIScope(t *testing.T) {
	for _, scope := range AllAPIScopes() {
		if !IsValidAPIScope(scope) {
			t.Fatalf("IsValidAPIScope(%q) = false, want true", scope)
		}
	}
	for _, scope := range []APIScope{"", "read", "documents", "admin", "search"} {
		if IsValidAPIScope(scope) {
			t.Fatalf("IsValidAPIScope(%q) = true, want false", scope)
		}
	}
}

func TestDeriveAPIKeyStatus(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	revoke := now.Add(-time.Hour)
	cases := []struct {
		name      string
		revokedAt *time.Time
		expiresAt *time.Time
		want      APIKeyStatus
	}{
		{name: "revoked wins over expired", revokedAt: &revoke, expiresAt: ptrTime(now.Add(-time.Hour)), want: APIKeyStatusRevoked},
		{name: "never expires is active", revokedAt: nil, expiresAt: nil, want: APIKeyStatusActive},
		{name: "expired", revokedAt: nil, expiresAt: ptrTime(now.Add(-time.Minute)), want: APIKeyStatusExpired},
		{name: "expiring within 7d", revokedAt: nil, expiresAt: ptrTime(now.Add(3 * 24 * time.Hour)), want: APIKeyStatusExpiring},
		{name: "active beyond 7d", revokedAt: nil, expiresAt: ptrTime(now.Add(30 * 24 * time.Hour)), want: APIKeyStatusActive},
		{name: "exactly now is expired", revokedAt: nil, expiresAt: &now, want: APIKeyStatusExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveAPIKeyStatus(tc.revokedAt, tc.expiresAt, now); got != tc.want {
				t.Fatalf("DeriveAPIKeyStatus() = %q, want %q", got, tc.want)
			}
		})
	}
	// NULL expiry 永不因时间变为 expired。
	if got := DeriveAPIKeyStatus(nil, nil, now.Add(100*365*24*time.Hour)); got != APIKeyStatusActive {
		t.Fatalf("never-expiring key far future = %q, want active", got)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
