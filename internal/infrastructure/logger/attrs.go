package logger

import (
	"log/slog"

	"github.com/google/uuid"
)

// 琅嬛结构化日志的字段名约定。所有模块打 lineage / 运维字段时统一引用这些常量，
// 避免散落的字符串字面量导致字段名漂移（如 "workspace_id" vs "workspaceId"）。
const (
	// lineage：资源血缘链。
	FieldWorkspaceID        = "workspace_id"
	FieldKnowledgeBaseID    = "knowledge_base_id"
	FieldDocumentID         = "document_id"
	FieldRevisionID         = "document_revision_id"
	FieldJobID              = "job_id"
	FieldIndexGenerationID  = "index_generation_id"
	FieldChunkSetID         = "chunk_set_id"
	FieldSourceConnectionID = "source_connection_id"

	// 运维：请求与执行上下文。
	FieldRequestID     = "request_id"
	FieldTransport     = "transport"
	FieldPrincipalKind = "principal_kind"
	FieldEvent         = "event"
	FieldStage         = "stage"
	FieldStatus        = "status"
	FieldDurationMS    = "duration_ms"
	FieldErrorClass    = "error_class"
	FieldAttempt       = "attempt"
)

// LineageAttrs 构造一组 lineage slog 字段。零值 UUID 被跳过，避免日志出现
// "00000000-..." 噪声。调用方按需传入非空 ID。
func LineageAttrs(workspaceID, knowledgeBaseID, documentID, revisionID, jobID uuid.UUID) []any {
	attrs := make([]any, 0, 5)
	if workspaceID != uuid.Nil {
		attrs = append(attrs, slog.String(FieldWorkspaceID, workspaceID.String()))
	}
	if knowledgeBaseID != uuid.Nil {
		attrs = append(attrs, slog.String(FieldKnowledgeBaseID, knowledgeBaseID.String()))
	}
	if documentID != uuid.Nil {
		attrs = append(attrs, slog.String(FieldDocumentID, documentID.String()))
	}
	if revisionID != uuid.Nil {
		attrs = append(attrs, slog.String(FieldRevisionID, revisionID.String()))
	}
	if jobID != uuid.Nil {
		attrs = append(attrs, slog.String(FieldJobID, jobID.String()))
	}
	return attrs
}

// StageAttrs 构造导入阶段耗时的标准 slog 字段组。
func StageAttrs(stage, status string, durationMS int64) []any {
	return []any{
		slog.String(FieldEvent, "pipeline.stage.completed"),
		slog.String(FieldStage, stage),
		slog.String(FieldStatus, status),
		slog.Int64(FieldDurationMS, durationMS),
	}
}
