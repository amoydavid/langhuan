package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// TaskChunkRevisionIndex is the targeted immutable ChunkRevision indexing task.
const TaskChunkRevisionIndex = "chunk_revision_index"

// ChunkRevisionTaskPayload carries the complete CAS and tenant lineage.
type ChunkRevisionTaskPayload struct {
	WorkspaceID            uuid.UUID `json:"workspace_id"`
	KnowledgeBaseID        uuid.UUID `json:"knowledge_base_id"`
	GenerationID           uuid.UUID `json:"generation_id"`
	DocumentID             uuid.UUID `json:"document_id"`
	DocumentRevisionID     uuid.UUID `json:"document_revision_id"`
	ChunkSetID             uuid.UUID `json:"chunk_set_id"`
	ChunkID                uuid.UUID `json:"chunk_id"`
	BaseRevisionID         uuid.UUID `json:"base_revision_id"`
	NewRevisionID          uuid.UUID `json:"new_revision_id"`
	ExpectedContentVersion int64     `json:"expected_content_version"`
	JobID                  uuid.UUID `json:"job_id"`
}

// ChunkRevisionIndexer is the application use case invoked by the worker adapter.
type ChunkRevisionIndexer interface {
	Run(context.Context, appservice.ChunkRevisionIndexRequest) error
}

// ChunkRevisionHandler decodes the queue protocol and forwards to the application service.
type ChunkRevisionHandler struct {
	Indexer ChunkRevisionIndexer
	Logger  *slog.Logger
}

// RegisterChunkRevisionHandler registers the targeted indexing consumer.
func RegisterChunkRevisionHandler(mux *asynq.ServeMux, handler ChunkRevisionHandler) {
	mux.HandleFunc(TaskChunkRevisionIndex, handler.Handle)
}

func (h ChunkRevisionHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// Handle validates the full payload before invoking the application layer.
func (h ChunkRevisionHandler) Handle(ctx context.Context, task *asynq.Task) error {
	if h.Indexer == nil {
		return fmt.Errorf("ChunkRevision indexer 不能为空")
	}
	payload, err := decodeChunkRevisionTaskPayload(task)
	if err != nil {
		return err
	}
	err = h.Indexer.Run(ctx, appservice.ChunkRevisionIndexRequest{
		WorkspaceID: payload.WorkspaceID, KnowledgeBaseID: payload.KnowledgeBaseID,
		GenerationID: payload.GenerationID, DocumentID: payload.DocumentID,
		DocumentRevisionID: payload.DocumentRevisionID, ChunkSetID: payload.ChunkSetID,
		ChunkID: payload.ChunkID, BaseRevisionID: payload.BaseRevisionID,
		NewRevisionID: payload.NewRevisionID, ExpectedContentVersion: payload.ExpectedContentVersion,
		JobID: payload.JobID,
	})
	if err != nil {
		h.logger().LogAttrs(ctx, slog.LevelError, "ChunkRevision 索引失败",
			slog.String("workspace_id", payload.WorkspaceID.String()),
			slog.String("document_id", payload.DocumentID.String()),
			slog.String("revision_id", payload.DocumentRevisionID.String()),
			slog.String("chunk_id", payload.ChunkID.String()),
			slog.String("job_id", payload.JobID.String()),
			slog.String("error", err.Error()))
		if isPermanentChunkRevisionTaskError(err) {
			return errors.Join(asynq.SkipRetry, err)
		}
		return err
	}
	h.logger().LogAttrs(ctx, slog.LevelDebug, "ChunkRevision 索引完成",
		slog.String("workspace_id", payload.WorkspaceID.String()),
		slog.String("document_id", payload.DocumentID.String()),
		slog.String("revision_id", payload.DocumentRevisionID.String()),
		slog.String("chunk_id", payload.ChunkID.String()),
		slog.String("job_id", payload.JobID.String()))
	return nil
}

func decodeChunkRevisionTaskPayload(task *asynq.Task) (ChunkRevisionTaskPayload, error) {
	var payload ChunkRevisionTaskPayload
	if task == nil {
		return payload, fmt.Errorf("ChunkRevision task 不能为空")
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return payload, fmt.Errorf("解析 ChunkRevision task payload 失败: %w", err)
	}
	ids := []struct {
		name string
		id   uuid.UUID
	}{
		{name: "workspace_id", id: payload.WorkspaceID},
		{name: "knowledge_base_id", id: payload.KnowledgeBaseID},
		{name: "generation_id", id: payload.GenerationID},
		{name: "document_id", id: payload.DocumentID},
		{name: "document_revision_id", id: payload.DocumentRevisionID},
		{name: "chunk_set_id", id: payload.ChunkSetID},
		{name: "chunk_id", id: payload.ChunkID},
		{name: "base_revision_id", id: payload.BaseRevisionID},
		{name: "new_revision_id", id: payload.NewRevisionID},
		{name: "job_id", id: payload.JobID},
	}
	for _, item := range ids {
		if item.id == uuid.Nil {
			return payload, fmt.Errorf("ChunkRevision task %s 不能为空", item.name)
		}
	}
	if payload.ExpectedContentVersion < 0 {
		return payload, fmt.Errorf("ChunkRevision task expected_content_version 不能为负数")
	}
	return payload, nil
}

func isPermanentChunkRevisionTaskError(err error) bool {
	return errors.Is(err, domainerrors.ErrValidation) ||
		errors.Is(err, domainerrors.ErrRevisionConflict) ||
		errors.Is(err, domainerrors.ErrGenerationStale) ||
		errors.Is(err, domainerrors.ErrNotFound)
}
