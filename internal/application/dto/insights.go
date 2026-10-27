package dto

import (
	"time"

	"github.com/google/uuid"
)

// InsightsSleepingDocument 是健康面板沉睡内容清单的一行（v1.4.0）。
type InsightsSleepingDocument struct {
	DocumentID uuid.UUID `json:"document_id"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	// IndexedAt 用 documents.updated_at 近似"就绪时间"（无独立索引时间列）。
	IndexedAt time.Time `json:"indexed_at"`
}

// InsightsActivityEvent 是内容动态 feed 的单条事件。
// Kind: sync（源同步终态）| revision（内容新版本）| delete（软删）。
type InsightsActivityEvent struct {
	Kind             string     `json:"kind"`
	TargetDocumentID uuid.UUID  `json:"target_document_id,omitempty"`
	SourceID         *uuid.UUID `json:"source_id,omitempty"`
	SourceName       string     `json:"source_name,omitempty"`
	DocumentKind     string     `json:"document_kind,omitempty"`
	DocumentTitle    string     `json:"document_title,omitempty"`
	RevisionReason   string     `json:"revision_reason,omitempty"`
	Success          bool       `json:"success"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	OccurredAt       time.Time  `json:"occurred_at"`
}
