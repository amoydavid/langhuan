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
