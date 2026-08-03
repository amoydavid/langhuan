package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

var ErrInvalidRawDocumentKey = errors.New("invalid raw document key")

type RawDocumentStore struct {
	root string
}

func NewRawDocumentStore(root string) *RawDocumentStore {
	return &RawDocumentStore{root: filepath.Clean(root)}
}

func (s *RawDocumentStore) Put(ctx context.Context, input portstorage.RawDocumentInput) (*portstorage.RawDocumentObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.Reader == nil {
		return nil, errors.New("raw document reader is nil")
	}

	name := safeFileName(input.FileName)
	key := filepath.ToSlash(filepath.Join(
		input.WorkspaceID.String(),
		input.KnowledgeBaseID.String(),
		input.DocumentID.String(),
		name,
	))
	targetPath, err := s.pathForKey(key)
	if err != nil {
		return nil, err
	}
	targetDir := filepath.Dir(targetPath)
	if err := s.rejectExistingSymlinkComponents(targetDir, true); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("create raw document directory: %w", err)
	}
	if err := s.rejectExistingSymlinkComponents(targetPath, true); err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(targetDir, "."+name+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create raw document temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), input.Reader)
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("write raw document: %w", copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close raw document temp file: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return nil, fmt.Errorf("chmod raw document temp file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return nil, fmt.Errorf("store raw document: %w", err)
	}
	removeTemp = false

	return &portstorage.RawDocumentObject{
		Key:         key,
		SizeBytes:   written,
		SHA256:      hex.EncodeToString(hash.Sum(nil)),
		ContentType: input.ContentType,
	}, nil
}

func (s *RawDocumentStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
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
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (s *RawDocumentStore) Delete(ctx context.Context, key string) error {
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
		return err
	}
	return ctx.Err()
}

func (s *RawDocumentStore) pathForKey(key string) (string, error) {
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
		return "", fmt.Errorf("resolve raw document root: %w", err)
	}
	pathAbs, err := filepath.Abs(filepath.Join(rootAbs, cleanKey))
	if err != nil {
		return "", fmt.Errorf("resolve raw document path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return "", fmt.Errorf("validate raw document path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrInvalidRawDocumentKey
	}
	return pathAbs, nil
}

func (s *RawDocumentStore) rejectExistingSymlinkComponents(path string, includeTarget bool) error {
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("resolve raw document root: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve raw document path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("validate raw document path: %w", err)
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
			return fmt.Errorf("inspect raw document path component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink path component", ErrInvalidRawDocumentKey)
		}
	}
	return nil
}

func safeFileName(name string) string {
	base := filepath.Base(name)
	var b strings.Builder
	for _, r := range base {
		if isSafeFileNameRune(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	safe := strings.Trim(b.String(), "._")
	if safe == "" {
		return "document"
	}
	return safe
}

func isSafeFileNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
}
