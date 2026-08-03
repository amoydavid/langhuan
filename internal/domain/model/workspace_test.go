package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestNewWorkspaceRequiresName(t *testing.T) {
	if _, err := NewWorkspace("", "acme", nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("expected validation error for name, got %v", err)
	}
}

func TestNewWorkspaceNormalizesMetadata(t *testing.T) {
	workspace, err := NewWorkspace("Acme", "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.ID == uuid.Nil {
		t.Fatal("workspace ID should be generated")
	}
	if workspace.Metadata == nil {
		t.Fatal("metadata should default to an empty map")
	}
	if len(workspace.Metadata) != 0 {
		t.Fatalf("metadata = %#v", workspace.Metadata)
	}
	if workspace.Slug != "acme" {
		t.Fatalf("slug = %q, want %q", workspace.Slug, "acme")
	}
}

func TestNewWorkspaceRejectsInvalidSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
	}{
		{name: "empty", slug: ""},
		{name: "uppercase", slug: "Acme"},
		{name: "leading hyphen", slug: "-acme"},
		{name: "trailing hyphen", slug: "acme-"},
		{name: "too short length 1", slug: "a"},
		{name: "too short length 2", slug: "ab"},
		{name: "too long length 65", slug: "a" + strings.Repeat("b", 63) + "c"},
		{name: "space", slug: "acme corp"},
		{name: "underscore", slug: "acme_corp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWorkspace("Acme", tt.slug, nil)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("slug %q expected validation error, got %v", tt.slug, err)
			}
		})
	}
}

func TestNewWorkspaceAcceptsValidSlugs(t *testing.T) {
	// 规格要求 slug 长度为 3~64，小写字母/数字开头结尾、中间可含连字符。
	valid := []string{
		"abc",                         // length 3 (min)
		"a1b",                         // alphanumeric
		"acme",                        // length 4
		"acme-corp",                   // with hyphen
		"a" + strings.Repeat("c", 62), // length 63
		strings.Repeat("a", 63),       // length 63
		strings.Repeat("a", 64),       // length 64 (max)
	}
	for _, slug := range valid {
		t.Run(slug, func(t *testing.T) {
			ws, err := NewWorkspace("Acme", slug, nil)
			if err != nil {
				t.Fatalf("slug %q expected ok, got %v", slug, err)
			}
			if ws.Slug != slug {
				t.Fatalf("slug = %q, want %q", ws.Slug, slug)
			}
		})
	}
}
