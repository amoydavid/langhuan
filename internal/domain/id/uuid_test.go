package id

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewReturnsUUIDv7(t *testing.T) {
	got := New()
	if got == uuid.Nil {
		t.Fatal("New() returned nil UUID")
	}
	if got.Version() != 7 {
		t.Fatalf("New() version = %d, want 7", got.Version())
	}
}

func TestNewReturnsUniqueValues(t *testing.T) {
	seen := make(map[uuid.UUID]bool)
	for i := 0; i < 1000; i++ {
		got := New()
		if seen[got] {
			t.Fatalf("New() returned duplicate UUID %s", got)
		}
		seen[got] = true
	}
}

func TestNewIsRoughlyMonotonic(t *testing.T) {
	// UUIDv7 时间戳前缀应当单调递增（同毫秒内可相等）。
	// 直接比较 string 形式：时间戳在同一毫秒内生成时前缀相同，跨毫秒时递增。
	prev := New().String()
	for i := 0; i < 1000; i++ {
		next := New().String()
		if next < prev {
			t.Fatalf("UUIDv7 timestamp prefix went backwards: %s < %s", next, prev)
		}
		prev = next
	}
}
