package storage

import (
	"context"
	"io"
)

type ObjectInput struct {
	Key      string
	MimeType string
	Data     []byte
	Metadata map[string]any
}

type StoredObject struct {
	Key       string
	PublicURL string
	SizeBytes int64
	SHA256    string
}

type AssetStore interface {
	Put(ctx context.Context, object ObjectInput) (*StoredObject, error)
	Delete(ctx context.Context, key string) error
	// Open 按 storage key 打开已归档的资产内容，供鉴权代理 handler 读取。
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}
