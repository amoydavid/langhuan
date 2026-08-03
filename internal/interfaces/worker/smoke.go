package worker

import (
	"context"

	"github.com/hibiken/asynq"
)

const TypeSystemSmoke = "system_smoke"

func RegisterSmokeHandler(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeSystemSmoke, HandleSystemSmoke)
}

func HandleSystemSmoke(_ context.Context, _ *asynq.Task) error {
	return nil
}
