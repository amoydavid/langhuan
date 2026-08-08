package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	portstorage "github.com/dajee/langhuan/internal/ports/storage"
	"github.com/google/uuid"
)

func TestAssetStorePutComputesHashAndURL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewAssetStore(dir)

	workspaceID := uuid.New()
	docID := uuid.New()
	revisionID := uuid.New()
	assetID := uuid.New()
	key := strings.Join([]string{"assets", workspaceID.String(), docID.String(), revisionID.String(), assetID.String() + ".png"}, "/")

	data := []byte("\x89PNG\r\n\x1a\n fake png")
	obj, err := store.Put(ctx, portstorage.ObjectInput{
		Key:      key,
		MimeType: "image/png",
		Data:     data,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if obj.Key != key {
		t.Fatalf("Key = %q, want %q", obj.Key, key)
	}
	if obj.SizeBytes != int64(len(data)) {
		t.Fatalf("SizeBytes = %d, want %d", obj.SizeBytes, len(data))
	}
	sum := sha256.Sum256(data)
	if obj.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %q, want %q", obj.SHA256, hex.EncodeToString(sum[:]))
	}
	// local 模式 public URL 就是 key 本身（前端走后端代理 handler）
	if obj.PublicURL != key {
		t.Fatalf("PublicURL = %q, want %q", obj.PublicURL, key)
	}

	// 验证文件确实落盘
	targetPath := filepath.Join(dir, filepath.FromSlash(key))
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("stored content mismatch")
	}
}

func TestAssetStoreDelete(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewAssetStore(dir)

	key := "assets/ws/doc/rev/asset.png"
	if _, err := store.Put(ctx, portstorage.ObjectInput{
		Key:      key,
		MimeType: "image/png",
		Data:     []byte("png data"),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	targetPath := filepath.Join(dir, filepath.FromSlash(key))
	if _, err := os.Stat(targetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat() error = %v, want not exist", err)
	}
}

func TestAssetStorePutRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outside := t.TempDir()
	store := NewAssetStore(dir)

	// 在 root 下创建一个指向外部目录的 symlink
	if err := os.Symlink(outside, filepath.Join(dir, "assets")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := store.Put(ctx, portstorage.ObjectInput{
		Key:      "assets/ws/doc/rev/evil.png",
		MimeType: "image/png",
		Data:     []byte("evil"),
	})
	if err == nil {
		t.Fatal("Put() error = nil, want symlink rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Put() error = %v, want ErrInvalidRawDocumentKey", err)
	}

	// 确认文件没写到外部目录
	outsideTarget := filepath.Join(outside, "ws", "doc", "rev", "evil.png")
	if _, statErr := os.Stat(outsideTarget); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside target should not exist, stat error = %v", statErr)
	}
}

func TestAssetStoreRejectsEmptyKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewAssetStore(dir)

	_, err := store.Put(ctx, portstorage.ObjectInput{
		Key:      "",
		MimeType: "image/png",
		Data:     []byte("data"),
	})
	if err == nil {
		t.Fatal("Put() error = nil, want empty key rejection")
	}
}

func TestAssetStoreDeleteRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewAssetStore(dir)

	err := store.Delete(ctx, "../outside.png")
	if err == nil {
		t.Fatal("Delete() error = nil, want traversal rejection")
	}
	if !errors.Is(err, ErrInvalidRawDocumentKey) {
		t.Fatalf("Delete() error = %v, want ErrInvalidRawDocumentKey", err)
	}
}

func TestAssetStoreDeleteMissingMapsSentinel(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewAssetStore(dir)

	err := store.Delete(ctx, "assets/missing/asset.png")
	if !errors.Is(err, portstorage.ErrObjectNotFound) {
		t.Fatalf("Delete() error = %v, want ErrObjectNotFound", err)
	}
}

func TestAssetStoreOpenMissingMapsSentinel(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewAssetStore(dir)

	_, err := store.Open(ctx, "assets/missing/asset.png")
	if !errors.Is(err, portstorage.ErrObjectNotFound) {
		t.Fatalf("Open() error = %v, want ErrObjectNotFound", err)
	}
}
