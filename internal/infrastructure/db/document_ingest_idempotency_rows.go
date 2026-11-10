package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/service"
)

// DocumentIngestIdempotencyRow maps one idempotent document-ingest replay row.
// The natural key (workspace_id, api_key_id, knowledge_base_id, key) is enforced
// by a unique index created in migration 000021.
type DocumentIngestIdempotencyRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null;index"`
	APIKeyID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID uuid.UUID `gorm:"type:uuid;not null;index"`
	Key             string    `gorm:"type:text;not null"`
	RequestSHA256   string    `gorm:"type:text;not null"`
	DocumentID      uuid.UUID `gorm:"type:uuid;not null;index"`
	JobID           uuid.UUID `gorm:"type:uuid;not null;index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (DocumentIngestIdempotencyRow) TableName() string { return "document_ingest_idempotencies" }

func documentIngestIdempotencyToRow(record service.DocumentIngestIdempotency) *DocumentIngestIdempotencyRow {
	return &DocumentIngestIdempotencyRow{
		// 主键由存储层生成：service 结构只承载重放语义、刻意不携带 ID，且
		// SQLite 方言没有 gen_random_uuid() 默认值；若留空会让每条插入都用
		// 零值 UUID，第一条占位后所有后续幂等记录永远撞主键（409）。
		ID:              uuid.New(),
		WorkspaceID:     record.WorkspaceID,
		APIKeyID:        record.APIKeyID,
		KnowledgeBaseID: record.KnowledgeBaseID,
		Key:             record.Key,
		RequestSHA256:   record.RequestSHA256,
		DocumentID:      record.DocumentID,
		JobID:           record.JobID,
	}
}

func documentIngestIdempotencyFromRow(row DocumentIngestIdempotencyRow) service.DocumentIngestIdempotency {
	return service.DocumentIngestIdempotency{
		WorkspaceID:     row.WorkspaceID,
		APIKeyID:        row.APIKeyID,
		KnowledgeBaseID: row.KnowledgeBaseID,
		Key:             row.Key,
		RequestSHA256:   row.RequestSHA256,
		DocumentID:      row.DocumentID,
		JobID:           row.JobID,
	}
}
