package s3

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRawDocumentKeyIgnoresPathTraversalFileName(t *testing.T) {
	ws := uuid.New()
	kb := uuid.New()
	doc := uuid.New()
	rev := uuid.New()
	got := RawDocumentKey(ws, kb, doc, rev, "../../evil.pdf")
	if !strings.HasSuffix(got, "/original.pdf") {
		t.Fatalf("key = %q, want suffix /original.pdf", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("key contains traversal: %q", got)
	}
	// 确认所有 UUID segment 都在 key 里
	for _, id := range []string{ws.String(), kb.String(), doc.String(), rev.String()} {
		if !strings.Contains(got, id) {
			t.Fatalf("key %q missing segment %s", got, id)
		}
	}
	if !strings.HasPrefix(got, "raw-documents/") {
		t.Fatalf("key = %q, want raw-documents/ prefix", got)
	}
}

func TestRawDocumentKeyFallsBackToBinForUnknownExt(t *testing.T) {
	ws := uuid.New()
	kb := uuid.New()
	doc := uuid.New()
	rev := uuid.New()
	got := RawDocumentKey(ws, kb, doc, rev, "noextension")
	if !strings.HasSuffix(got, "/original.bin") {
		t.Fatalf("key = %q, want suffix /original.bin", got)
	}
}

func TestRawMarkdownKeyShape(t *testing.T) {
	ws := uuid.New()
	doc := uuid.New()
	rev := uuid.New()
	job := uuid.New()
	got := RawMarkdownKey(ws, doc, rev, job)
	expected := "parser-results/" + strings.Join([]string{
		ws.String(), doc.String(), rev.String(), job.String(), "raw.md",
	}, "/")
	if got != expected {
		t.Fatalf("key = %q, want %q", got, expected)
	}
}

func TestAssetKeyDerivesExtFromMime(t *testing.T) {
	ws := uuid.New()
	doc := uuid.New()
	rev := uuid.New()
	asset := uuid.New()

	cases := []struct {
		mime   string
		suffix string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/webp", ".webp"},
		{"image/gif", ".gif"},
		{"", ".bin"},
	}
	for _, tc := range cases {
		got := AssetKey(ws, doc, rev, asset, tc.mime, "")
		if !strings.HasSuffix(got, tc.suffix) {
			t.Fatalf("mime %q: key = %q, want suffix %s", tc.mime, got, tc.suffix)
		}
	}
}

func TestAssetKeyUsesFileNameWhenMimeUnknown(t *testing.T) {
	ws := uuid.New()
	doc := uuid.New()
	rev := uuid.New()
	asset := uuid.New()
	got := AssetKey(ws, doc, rev, asset, "", "photo.tiff")
	if !strings.HasSuffix(got, ".tiff") {
		t.Fatalf("key = %q, want .tiff suffix", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("key contains traversal: %q", got)
	}
}

func TestAssetKeyContainsAllSegments(t *testing.T) {
	ws := uuid.New()
	doc := uuid.New()
	rev := uuid.New()
	asset := uuid.New()
	got := AssetKey(ws, doc, rev, asset, "image/png", "")
	for _, id := range []string{ws.String(), doc.String(), rev.String(), asset.String()} {
		if !strings.Contains(got, id) {
			t.Fatalf("key %q missing segment %s", got, id)
		}
	}
	if !strings.HasPrefix(got, "assets/") {
		t.Fatalf("key = %q, want assets/ prefix", got)
	}
}
