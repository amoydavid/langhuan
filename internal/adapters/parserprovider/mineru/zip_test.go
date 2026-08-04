package mineru

import (
	"archive/zip"
	"bytes"
	"testing"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// buildTestZip 在内存中构造一个包含 markdown 与图片的 zip。
func buildTestZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractImagesFromZip(t *testing.T) {
	zipData := buildTestZip(t, map[string][]byte{
		"full.md":          []byte("![图](images/logo.png)\n\n正文"),
		"images/logo.png":  []byte("png-bytes"),
		"images/photo.jpg": []byte("jpg-bytes"),
		"images/notes.txt": []byte("not an image"),
		"main.md":          []byte("另一个 markdown"),
	})

	assets, err := extractImagesFromZip(zipData, 10*1024*1024)
	if err != nil {
		t.Fatalf("extractImagesFromZip error = %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("assets = %d, want 2 (png + jpg)", len(assets))
	}

	byPath := make(map[string]zipAsset, len(assets))
	for _, a := range assets {
		byPath[a.RelativePath] = a
	}

	png, ok := byPath["images/logo.png"]
	if !ok {
		t.Fatal("missing images/logo.png")
	}
	if png.Name != "logo.png" {
		t.Fatalf("Name = %q", png.Name)
	}
	if png.MimeType != "image/png" {
		t.Fatalf("MimeType = %q", png.MimeType)
	}
	if string(png.Data) != "png-bytes" {
		t.Fatalf("Data = %q", png.Data)
	}

	jpg, ok := byPath["images/photo.jpg"]
	if !ok {
		t.Fatal("missing images/photo.jpg")
	}
	if jpg.MimeType != "image/jpeg" {
		t.Fatalf("jpg MimeType = %q", jpg.MimeType)
	}
}

func TestExtractImagesFromZipSkipsOversized(t *testing.T) {
	zipData := buildTestZip(t, map[string][]byte{
		"images/big.png": []byte("a-lot-of-png-data"),
	})

	// 上限 8 字节，big.png 应被跳过
	assets, err := extractImagesFromZip(zipData, 8)
	if err != nil {
		t.Fatalf("extractImagesFromZip error = %v", err)
	}
	if len(assets) != 0 {
		t.Fatalf("assets = %d, want 0 (oversized)", len(assets))
	}
}

func TestExtractImagesFromZipNonZipReturnsNil(t *testing.T) {
	// 非 zip 数据应返回 nil candidates 而不是 error（不阻断解析主流程）
	candidates := extractAssetsFromZip([]byte("not a zip at all"), 10*1024*1024)
	if candidates != nil {
		t.Fatalf("candidates = %#v, want nil", candidates)
	}
}

func TestExtractAssetsFromZipMapsCandidates(t *testing.T) {
	zipData := buildTestZip(t, map[string][]byte{
		"full.md":             []byte("![图](images/diagram.webp)"),
		"images/diagram.webp": []byte("webp-bytes"),
	})

	candidates := extractAssetsFromZip(zipData, 10*1024*1024)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	cand := candidates[0]
	if cand.RelativePath != "images/diagram.webp" {
		t.Fatalf("RelativePath = %q", cand.RelativePath)
	}
	if cand.MimeType != "image/webp" {
		t.Fatalf("MimeType = %q", cand.MimeType)
	}
	if string(cand.Data) != "webp-bytes" {
		t.Fatalf("Data = %q", cand.Data)
	}

	// 确保类型与 ports 定义一致，供 AssetResolver 使用
	var _ []parserport.AssetCandidate = candidates
}
