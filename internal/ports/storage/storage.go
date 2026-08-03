package storage

import "context"

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
}
