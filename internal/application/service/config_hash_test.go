package service

import (
	"encoding/json"
	"math"
	"testing"
)

func TestCanonicalConfigHashIgnoresMapOrder(t *testing.T) {
	a := map[string]any{"chunk_size": 512, "nested": map[string]any{"b": 2, "a": 1}}
	b := map[string]any{"nested": map[string]any{"a": 1, "b": 2}, "chunk_size": 512}
	hashA, err := CanonicalConfigHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := CanonicalConfigHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB || len(hashA) != 64 {
		t.Fatalf("hashes = %q %q", hashA, hashB)
	}
}

func TestCanonicalConfigHashRejectsNonJSONNumbers(t *testing.T) {
	if _, err := CanonicalConfigHash(map[string]any{"invalid": math.NaN()}); err == nil {
		t.Fatal("NaN unexpectedly accepted")
	}
}

func TestCanonicalConfigHashRejectsInvalidJSONNumber(t *testing.T) {
	if _, err := CanonicalConfigHash(map[string]any{"invalid": json.Number("NaN")}); err == nil {
		t.Fatal("json.Number NaN unexpectedly accepted")
	}
}
