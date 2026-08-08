// Package local 的 AssetStore 实现把解析图片资产写入本地文件系统。
// 它与 RawDocumentStore 共享同一 root 目录下的 pathForKey / symlink 防护逻辑，
// 但 key 由调用方（AssetResolver / S3 key builder）直接提供，不从用户文件名派生。
package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

// AssetStore 把图片资产写入本地文件系统，复用 RawDocumentStore 的路径安全防护。
type AssetStore struct {
	root string
}

// NewAssetStore 创建本地资产存储，root 与 RawDocumentStore 可以相同或不同。
func NewAssetStore(root string) *AssetStore {
	return &AssetStore{root: filepath.Clean(root)}
}

func (s *AssetStore) Put(ctx context.Context, object portstorage.ObjectInput) (*portstorage.StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if object.Key == "" {
		return nil, errors.New("asset key is empty")
	}

	targetPath, err := s.pathForKey(object.Key)
	if err != nil {
		return nil, err
	}
	targetDir := filepath.Dir(targetPath)
	if err := s.rejectExistingSymlinkComponents(targetDir, true); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("create asset directory: %w", err)
	}
	if err := s.rejectExistingSymlinkComponents(targetPath, true); err != nil {
		return nil, err
	}

	name := filepath.Base(targetPath)
	tmp, err := os.CreateTemp(targetDir, "."+name+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create asset temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), bytes.NewReader(object.Data))
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("write asset: %w", copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close asset temp file: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return nil, fmt.Errorf("chmod asset temp file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return nil, fmt.Errorf("store asset: %w", err)
	}
	removeTemp = false

	return &portstorage.StoredObject{
		Key:       object.Key,
		PublicURL: assetPublicURL(s.root, object.Key),
		SizeBytes: written,
		SHA256:    hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (s *AssetStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	if err := s.rejectExistingSymlinkComponents(path, true); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return mapMissingObjectError(err)
	}
	return ctx.Err()
}

// Open 按 storage key 打开已归档的资产内容，供鉴权代理 handler 读取。
func (s *AssetStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.pathForKey(key)
	if err != nil {
		return nil, err
	}
	if err := s.rejectExistingSymlinkComponents(path, true); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, mapMissingObjectError(err)
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (s *AssetStore) pathForKey(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) {
		return "", ErrInvalidRawDocumentKey
	}

	nativeKey := filepath.FromSlash(key)
	if filepath.IsAbs(nativeKey) {
		return "", ErrInvalidRawDocumentKey
	}
	cleanKey := filepath.Clean(nativeKey)
	if cleanKey == "." || cleanKey == ".." || strings.HasPrefix(cleanKey, ".."+string(os.PathSeparator)) {
		return "", ErrInvalidRawDocumentKey
	}

	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", fmt.Errorf("resolve asset root: %w", err)
	}
	pathAbs, err := filepath.Abs(filepath.Join(rootAbs, cleanKey))
	if err != nil {
		return "", fmt.Errorf("resolve asset path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return "", fmt.Errorf("validate asset path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrInvalidRawDocumentKey
	}
	return pathAbs, nil
}

func (s *AssetStore) rejectExistingSymlinkComponents(path string, includeTarget bool) error {
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("resolve asset root: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve asset path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("validate asset path: %w", err)
	}
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ErrInvalidRawDocumentKey
	}

	parts := strings.Split(rel, string(os.PathSeparator))
	if !includeTarget && len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}

	current := rootAbs
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect asset path component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink path component", ErrInvalidRawDocumentKey)
		}
	}
	return nil
}

// assetPublicURL 在 local 模式返回相对 key 路径，前端通过后端资产代理 handler 访问。
// 实际的鉴权代理在 HTTP 层实现（见 v0.7.0 资产预览任务）。
func assetPublicURL(root, key string) string {
	return filepath.ToSlash(key)
}
