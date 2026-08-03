package worker

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
)

func TestRegisterSmokeHandler(t *testing.T) {
	mux := asynq.NewServeMux()
	RegisterSmokeHandler(mux)

	handler, pattern := mux.Handler(asynq.NewTask(TypeSystemSmoke, nil))
	if pattern != TypeSystemSmoke {
		t.Fatalf("pattern = %q", pattern)
	}
	if err := handler.ProcessTask(context.Background(), asynq.NewTask(TypeSystemSmoke, nil)); err != nil {
		t.Fatalf("ProcessTask error = %v", err)
	}
}
