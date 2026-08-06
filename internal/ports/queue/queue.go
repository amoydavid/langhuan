package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Delay 表示入队前的等待时间。零值表示立即执行。
type Delay time.Duration

type JobRequest struct {
	Type    string
	Payload []byte
	Queue   string
	TaskID  string
	Delay   Delay
}

type JobHandle struct {
	ID string
}

type JobQueue interface {
	Enqueue(ctx context.Context, job JobRequest) (*JobHandle, error)
}

// DocumentTaskID returns the stable identity shared by all producers of a document-stage task.
func DocumentTaskID(typ string, workspaceID, revisionID, generationID uuid.UUID) string {
	return fmt.Sprintf("%s:%s:%s:%s", typ, workspaceID, revisionID, generationID)
}

// DocumentPollTaskID returns the identity for a single async poll attempt.
// 包含 jobID，保证同一 revision 的每次轮询重入队都使用唯一 TaskID，
// 避免 asynq 的 "task ID conflicts with another task" 错误。
func DocumentPollTaskID(workspaceID, revisionID, jobID uuid.UUID) string {
	return fmt.Sprintf("poll:%s:%s:%s", workspaceID, revisionID, jobID)
}

// SourceSyncTaskID 返回 source_sync 任务的稳定 TaskID（幂等去重）。
// 按 KB 维度幂等：同一 KB 同时只允许一个 source_sync 任务在队列中。
func SourceSyncTaskID(workspaceID, kbID uuid.UUID) string {
	return fmt.Sprintf("source_sync:%s:%s", workspaceID, kbID)
}
