package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	appservice "github.com/dajee/langhuan/internal/application/service"
)

func TestIndexGenerationBuildTaskForwardsCompleteLineage(t *testing.T) {
	builder := &generationBuilderSpy{}
	handler := IndexGenerationBuildHandler{Builder: builder}
	payload := IndexGenerationBuildTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), GenerationID: uuid.New(), JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskIndexGenerationBuild, encoded)); err != nil {
		t.Fatal(err)
	}
	if len(builder.requests) != 1 || builder.requests[0].GenerationID != payload.GenerationID {
		t.Fatalf("requests = %#v", builder.requests)
	}
	if builder.requests[0].TerminalAttempt {
		t.Fatal("TerminalAttempt = true without asynq retry metadata")
	}
}

func TestIsFinalRetryAttempt(t *testing.T) {
	tests := []struct {
		name                           string
		retryCount, maxRetry           int
		retryCountOK, maxRetryOK, want bool
	}{
		{name: "before limit", retryCount: 1, maxRetry: 3, retryCountOK: true, maxRetryOK: true},
		{name: "at limit", retryCount: 3, maxRetry: 3, retryCountOK: true, maxRetryOK: true, want: true},
		{name: "past limit", retryCount: 4, maxRetry: 3, retryCountOK: true, maxRetryOK: true, want: true},
		{name: "missing retry count", maxRetry: 3, maxRetryOK: true},
		{name: "missing max retry", retryCount: 3, retryCountOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isFinalRetryAttempt(
				test.retryCount, test.retryCountOK, test.maxRetry, test.maxRetryOK,
			); got != test.want {
				t.Fatalf("isFinalRetryAttempt() = %v, want %v", got, test.want)
			}
		})
	}
}

type generationBuilderSpy struct {
	requests []appservice.IndexGenerationBuildRequest
}

func (s *generationBuilderSpy) Run(_ context.Context, request appservice.IndexGenerationBuildRequest) error {
	s.requests = append(s.requests, request)
	return nil
}
