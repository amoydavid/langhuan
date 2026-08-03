package queue

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type JobRequest struct {
	Type    string
	Payload []byte
	Queue   string
	TaskID  string
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
