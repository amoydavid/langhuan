package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// TaskIndexGenerationBuild is the full inactive Generation rebuild task.
const TaskIndexGenerationBuild = "index_generation_build"

// IndexGenerationBuildTaskPayload carries complete tenant/build lineage.
type IndexGenerationBuildTaskPayload struct {
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	KnowledgeBaseID uuid.UUID `json:"knowledge_base_id"`
	GenerationID    uuid.UUID `json:"generation_id"`
	JobID           uuid.UUID `json:"job_id"`
}

// IndexGenerationBuilder is the application use case invoked by the worker adapter.
type IndexGenerationBuilder interface {
	Run(context.Context, appservice.IndexGenerationBuildRequest) error
}

// IndexGenerationBuildHandler decodes and forwards a full rebuild task.
type IndexGenerationBuildHandler struct{ Builder IndexGenerationBuilder }

// RegisterIndexGenerationBuildHandler registers the full rebuild consumer.
func RegisterIndexGenerationBuildHandler(mux *asynq.ServeMux, handler IndexGenerationBuildHandler) {
	mux.HandleFunc(TaskIndexGenerationBuild, handler.Handle)
}

// Handle validates the queue protocol before invoking the application layer.
func (h IndexGenerationBuildHandler) Handle(ctx context.Context, task *asynq.Task) error {
	if h.Builder == nil {
		return fmt.Errorf("IndexGeneration builder 不能为空")
	}
	var payload IndexGenerationBuildTaskPayload
	if task == nil {
		return fmt.Errorf("IndexGeneration build task 不能为空")
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("解析 IndexGeneration build payload 失败: %w", err)
	}
	if payload.WorkspaceID == uuid.Nil || payload.KnowledgeBaseID == uuid.Nil ||
		payload.GenerationID == uuid.Nil || payload.JobID == uuid.Nil {
		return fmt.Errorf("IndexGeneration build payload lineage 不能为空")
	}
	err := h.Builder.Run(ctx, appservice.IndexGenerationBuildRequest{
		WorkspaceID: payload.WorkspaceID, KnowledgeBaseID: payload.KnowledgeBaseID,
		GenerationID: payload.GenerationID, JobID: payload.JobID,
		TerminalAttempt: indexGenerationBuildTerminalAttempt(ctx),
	})
	if err != nil && isPermanentIndexGenerationBuildTaskError(err) {
		return errors.Join(asynq.SkipRetry, err)
	}
	return err
}

func indexGenerationBuildTerminalAttempt(ctx context.Context) bool {
	retryCount, retryCountOK := asynq.GetRetryCount(ctx)
	maxRetry, maxRetryOK := asynq.GetMaxRetry(ctx)
	return isFinalRetryAttempt(retryCount, retryCountOK, maxRetry, maxRetryOK)
}

func isFinalRetryAttempt(retryCount int, retryCountOK bool, maxRetry int, maxRetryOK bool) bool {
	return retryCountOK && maxRetryOK && retryCount >= maxRetry
}

func isPermanentIndexGenerationBuildTaskError(err error) bool {
	return errors.Is(err, domainerrors.ErrValidation) ||
		errors.Is(err, domainerrors.ErrDimensionMismatch) ||
		errors.Is(err, domainerrors.ErrGenerationStale) ||
		errors.Is(err, domainerrors.ErrGenerationNotReady) ||
		errors.Is(err, domainerrors.ErrNotFound)
}
