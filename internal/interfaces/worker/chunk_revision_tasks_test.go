package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	appservice "github.com/dajee/langhuan/internal/application/service"
)

func TestChunkRevisionTaskRequiresCompleteLineage(t *testing.T) {
	indexer := &chunkRevisionIndexerSpy{}
	handler := ChunkRevisionHandler{Indexer: indexer}
	payload, err := json.Marshal(ChunkRevisionTaskPayload{
		KnowledgeBaseID: uuid.New(), GenerationID: uuid.New(), DocumentID: uuid.New(),
		DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(), ChunkID: uuid.New(),
		BaseRevisionID: uuid.New(), NewRevisionID: uuid.New(), JobID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = handler.Handle(context.Background(), asynq.NewTask(TaskChunkRevisionIndex, payload))
	if err == nil || len(indexer.requests) != 0 {
		t.Fatalf("error=%v calls=%d", err, len(indexer.requests))
	}
}

func TestChunkRevisionTaskForwardsCompleteRequest(t *testing.T) {
	indexer := &chunkRevisionIndexerSpy{}
	handler := ChunkRevisionHandler{Indexer: indexer}
	payload := ChunkRevisionTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), GenerationID: uuid.New(),
		DocumentID: uuid.New(), DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(),
		ChunkID: uuid.New(), BaseRevisionID: uuid.New(), NewRevisionID: uuid.New(),
		ExpectedContentVersion: 9, JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskChunkRevisionIndex, encoded)); err != nil {
		t.Fatal(err)
	}
	if len(indexer.requests) != 1 || indexer.requests[0].NewRevisionID != payload.NewRevisionID ||
		indexer.requests[0].ExpectedContentVersion != 9 {
		t.Fatalf("requests = %#v", indexer.requests)
	}
}

type chunkRevisionIndexerSpy struct {
	requests []appservice.ChunkRevisionIndexRequest
}

func (s *chunkRevisionIndexerSpy) Run(_ context.Context, request appservice.ChunkRevisionIndexRequest) error {
	s.requests = append(s.requests, request)
	return nil
}
