package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	portstorage "github.com/dajee/langhuan/internal/ports/storage"
	"github.com/google/uuid"
)

func TestRawDocumentStorePutComputesHashAndPreventsTraversal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewRawDocumentStore(dir)

	workspaceID := uuid.New()
	kbID := uuid.New()
	docID := uuid.New()

	object, err := store.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID:     workspaceID,
		KnowledgeBaseID: kbID,
		DocumentID:      docID,
		FileName:        "../evil.pdf",
		ContentType:     "application/pdf",
		Reader:          strings.NewReader("hello"),
		SizeBytes:       int64(len("hello")),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	expectedPrefix := filepath.ToSlash(filepath.Join(workspaceID.String(), kbID.String(), docID.String()))
	if !strings.Contains(object.Key, expectedPrefix) {
		t.Fatalf("key %q does not contain %q", object.Key, expectedPrefix)
	}
	if strings.Contains(object.Key, "..") {
		t.Fatalf("key %q contains traversal segment", object.Key)
	}
	if object.SizeBytes != int64(len("hello")) {
		t.Fatalf("SizeBytes = %d, want %d", object.SizeBytes, len("hello"))
	}
	if object.ContentType != "application/pdf" {
		t.Fatalf("ContentType = %q, want application/pdf", object.ContentType)
	}

	sum := sha256.Sum256([]byte("hello"))
	if object.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %q, want %q", object.SHA256, hex.EncodeToString(sum[:]))
	}

	reader, err := store.Open(ctx, object.Key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("stored content = %q, want hello", string(data))
	}
}

func TestRawDocumentStoreOpenRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewRawDocumentStore(dir)

	escaped := filepath.Join(dir, "..", "outside.txt")
	if err := os.WriteFile(escaped, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := store.Open(ctx, "../outside.txt")
	if err == nil {
		t.Fatal("Open() error = nil, want traversal rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Open() error = %v, want ErrInvalidRawDocumentKey", err)
	}
}

func TestRawDocumentStoreDeleteRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewRawDocumentStore(dir)

	escaped := filepath.Join(dir, "..", "outside.txt")
	if err := os.WriteFile(escaped, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := store.Delete(ctx, "../outside.txt")
	if err == nil {
		t.Fatal("Delete() error = nil, want traversal rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Delete() error = %v, want ErrInvalidRawDocumentKey", err)
	}
	if _, statErr := os.Stat(escaped); statErr != nil {
		t.Fatalf("outside file was affected: %v", statErr)
	}
}

func TestRawDocumentStorePutRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outside := t.TempDir()
	store := NewRawDocumentStore(dir)

	workspaceID := uuid.New()
	kbID := uuid.New()
	docID := uuid.New()
	if err := os.Symlink(outside, filepath.Join(dir, workspaceID.String())); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := store.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID:     workspaceID,
		KnowledgeBaseID: kbID,
		DocumentID:      docID,
		FileName:        "secret.pdf",
		ContentType:     "application/pdf",
		Reader:          strings.NewReader("secret"),
		SizeBytes:       int64(len("secret")),
	})
	if err == nil {
		t.Fatal("Put() error = nil, want symlink rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Put() error = %v, want ErrInvalidRawDocumentKey", err)
	}

	outsideTarget := filepath.Join(outside, kbID.String(), docID.String(), "secret.pdf")
	if _, statErr := os.Stat(outsideTarget); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside target stat error = %v, want not exist", statErr)
	}
}

func TestRawDocumentStoreOpenRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outside := t.TempDir()
	store := NewRawDocumentStore(dir)

	workspaceID := uuid.New()
	kbID := uuid.New()
	docID := uuid.New()
	outsideDir := filepath.Join(outside, kbID.String(), docID.String())
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.pdf"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, workspaceID.String())); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	key := filepath.ToSlash(filepath.Join(workspaceID.String(), kbID.String(), docID.String(), "secret.pdf"))
	reader, err := store.Open(ctx, key)
	if err == nil {
		_ = reader.Close()
		t.Fatal("Open() error = nil, want symlink rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Open() error = %v, want ErrInvalidRawDocumentKey", err)
	}
}

func TestRawDocumentStoreDeleteRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outside := t.TempDir()
	store := NewRawDocumentStore(dir)

	workspaceID := uuid.New()
	kbID := uuid.New()
	docID := uuid.New()
	outsideDir := filepath.Join(outside, kbID.String(), docID.String())
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.pdf")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, workspaceID.String())); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	key := filepath.ToSlash(filepath.Join(workspaceID.String(), kbID.String(), docID.String(), "secret.pdf"))
	err := store.Delete(ctx, key)
	if err == nil {
		t.Fatal("Delete() error = nil, want symlink rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Delete() error = %v, want ErrInvalidRawDocumentKey", err)
	}
	if data, readErr := os.ReadFile(outsideFile); readErr != nil || string(data) != "secret" {
		t.Fatalf("outside file changed, data = %q, error = %v", string(data), readErr)
	}
}

// putRawWithRevision 是一个小测试辅助函数：用固定的 workspace/kb/document 上下文写入一份原始内容，
// 仅替换 RevisionID。这样若无 revision 作用域，两次写入会落在同一个 key 上互相覆盖。
func putRawWithRevision(t *testing.T, store *RawDocumentStore, ws, kb, doc, rev uuid.UUID, content string) *portstorage.RawDocumentObject {
	t.Helper()
	ctx := context.Background()
	obj, err := store.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID:     ws,
		KnowledgeBaseID: kb,
		DocumentID:      doc,
		RevisionID:      rev,
		FileName:        "doc.md",
		ContentType:     "text/markdown",
		Reader:          strings.NewReader(content),
		SizeBytes:       int64(len(content)),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	return obj
}

func TestRawDocumentRevisionKeysDoNotOverwrite(t *testing.T) {
	t.Parallel()
	store := NewRawDocumentStore(t.TempDir())
	ws, kb, doc := uuid.New(), uuid.New(), uuid.New()
	first := putRawWithRevision(t, store, ws, kb, doc, uuid.New(), "one")
	second := putRawWithRevision(t, store, ws, kb, doc, uuid.New(), "two")
	if first.Key == second.Key {
		t.Fatalf("revision keys collide: %q", first.Key)
	}
	assertRawContent(t, store, first.Key, "one")
	assertRawContent(t, store, second.Key, "two")
}

func assertRawContent(t *testing.T, store *RawDocumentStore, key, want string) {
	t.Helper()
	reader, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", key, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != want {
		t.Fatalf("content = %q, want %q", string(data), want)
	}
}

func TestRawDocumentRevisionKeyShapeContainsRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewRawDocumentStore(t.TempDir())
	ws := uuid.New()
	kb := uuid.New()
	doc := uuid.New()
	rev := uuid.New()

	obj, err := store.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID: ws, KnowledgeBaseID: kb, DocumentID: doc, RevisionID: rev,
		FileName: "report.pdf", ContentType: "application/pdf",
		Reader: strings.NewReader("x"), SizeBytes: 1,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	expected := strings.Join([]string{ws.String(), kb.String(), doc.String(), rev.String(), "original.pdf"}, "/")
	if obj.Key != expected {
		t.Fatalf("key = %q, want %q", obj.Key, expected)
	}
}

// TestRawDocumentPutNilRevisionKeepsLegacyKey 验证当 RevisionID 为 uuid.Nil（未迁移的旧调用方）
// 时，key 沿用旧格式 {workspace}/{kb}/{doc}/{name}，保证存量 key 仍可寻址。
func TestRawDocumentPutNilRevisionKeepsLegacyKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewRawDocumentStore(t.TempDir())
	ws := uuid.New()
	kb := uuid.New()
	doc := uuid.New()

	obj, err := store.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID: ws, KnowledgeBaseID: kb, DocumentID: doc,
		FileName: "report.pdf", ContentType: "application/pdf",
		Reader: strings.NewReader("x"), SizeBytes: 1,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	expected := strings.Join([]string{ws.String(), kb.String(), doc.String(), "report.pdf"}, "/")
	if obj.Key != expected {
		t.Fatalf("legacy key = %q, want %q", obj.Key, expected)
	}
	// Open 必须仍能读取旧 key（Open 始终用完整 key）。
	assertRawContent(t, store, obj.Key, "x")
}

func TestLocalOpenMissingRawObjectMapsSentinel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewRawDocumentStore(t.TempDir())
	_, err := store.Open(ctx, "missing/object.md")
	if !errors.Is(err, portstorage.ErrObjectNotFound) {
		t.Fatalf("Open() error = %v, want ErrObjectNotFound", err)
	}
}

func TestLocalDeleteMissingRawObjectMapsSentinel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewRawDocumentStore(t.TempDir())
	err := store.Delete(ctx, "missing/object.md")
	if !errors.Is(err, portstorage.ErrObjectNotFound) {
		t.Fatalf("Delete() error = %v, want ErrObjectNotFound", err)
	}
}

func TestRawDocumentStoreRejectsInvalidKeys(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewRawDocumentStore(dir)

	for _, key := range []string{
		filepath.Join(dir, "absolute.pdf"),
		"a/../../x.pdf",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := store.Open(ctx, key)
			if err == nil {
				t.Fatal("Open() error = nil, want invalid key rejection")
			}
			if !errors.Is(err, ErrInvalidRawDocumentKey) {
				t.Fatalf("Open() error = %v, want ErrInvalidRawDocumentKey", err)
			}
		})
	}
}
