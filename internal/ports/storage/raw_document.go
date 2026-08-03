package storage

import (
	"context"
	"io"

	"github.com/google/uuid"
)

type RawDocumentInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	DocumentID      uuid.UUID
	FileName        string
	ContentType     string
	Reader          io.Reader
	SizeBytes       int64
}

type RawDocumentObject struct {
	Key         string
	SizeBytes   int64
	SHA256      string
	ContentType string
}

type RawDocumentStore interface {
	Put(ctx context.Context, input RawDocumentInput) (*RawDocumentObject, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
