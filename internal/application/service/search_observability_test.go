package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// memorySlogHandler 捕获所有日志记录用于断言。
type memorySlogHandler struct {
	records []map[string]any
	bytes   bytes.Buffer
}

func (h *memorySlogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *memorySlogHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{"event": "unknown", "msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	h.records = append(h.records, m)
	raw, _ := json.Marshal(m)
	h.bytes.Write(raw)
	h.bytes.WriteByte('\n')
	return nil
}

func (h *memorySlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *memorySlogHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *memorySlogHandler) countEvent(event string) int {
	count := 0
	for _, record := range h.records {
		if record["event"] == event {
			count++
		}
	}
	return count
}

func (h *memorySlogHandler) json() string { return h.bytes.String() }

func newCaptureLogger() (*slog.Logger, *memorySlogHandler) {
	handler := &memorySlogHandler{}
	return slog.New(handler), handler
}

func TestSearchLogsOneTerminalEventWithoutSensitiveText(t *testing.T) {
	workspaceID, kbID := uuid.New(), uuid.New()
	embeddingModelID, embeddingProviderID := uuid.New(), uuid.New()
	generation := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		EmbeddingModelID: embeddingModelID, ProviderID: embeddingProviderID,
		ModelName: "embed", EmbeddingDimension: 1024, ModelConfigHash: "ehash",
		ChunkerVersion: 1, RetrievalConfig: map[string]any{"fts_config": "simple", "vector_top_k": 30, "keyword_top_k": 30, "final_top_k": 10, "rrf_k": 60},
		Status: value.IndexGenerationReady,
	}
	entryID := uuid.New()
	repo := &searchRepositoryFake{
		generation: generation,
		vector:     []indexport.SearchCandidate{{EntryID: entryID, Score: 0.5}},
		evidence: map[uuid.UUID]indexport.SearchEvidence{
			entryID: {EntryID: entryID, ChunkID: uuid.New(), DocumentID: uuid.New(), Content: "绝密文档正文", SearchContent: "绝密检索片段", MatchedChunkID: entryID, MatchedSearchContent: "绝密检索片段", MatchedRole: value.ChunkRoleFlat},
		},
	}
	embeddingResolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: &chunkRevisionEmbeddingSpy{dimension: 1024}, ModelID: embeddingModelID, ProviderID: embeddingProviderID,
		ModelName: "embed", ModelConfigHash: "ehash", Dimensions: 1024, BatchSize: 32,
	}}
	logger, handler := newCaptureLogger()
	svc := NewSearchService(SearchServiceDeps{Repository: repo, Resolver: embeddingResolver, Logger: logger})

	_, err := svc.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: "绝密退款问题",
	})
	if err != nil {
		t.Fatalf("search err = %v", err)
	}
	got := handler.json()
	if handler.countEvent("search.completed") != 1 || handler.countEvent("search.failed") != 0 {
		t.Fatalf("terminal events = completed:%d failed:%d\n%s", handler.countEvent("search.completed"), handler.countEvent("search.failed"), got)
	}
	for _, secret := range []string{"绝密退款问题", "绝密文档正文", "绝密检索片段", "api_key"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log leaked secret %q: %s", secret, got)
		}
	}
}

func TestSearchLogsFailedEventOnGenerationNotReady(t *testing.T) {
	workspaceID, kbID := uuid.New(), uuid.New()
	// activeGeneration 校验失败时返回 ErrGenerationNotReady，应记录 search.failed。
	repo := &searchRepositoryFake{
		generation: &model.IndexGeneration{
			ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
			Status: value.IndexGenerationBuilding,
		},
	}
	embeddingResolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: &chunkRevisionEmbeddingSpy{dimension: 1024},
	}}
	logger, handler := newCaptureLogger()
	svc := NewSearchService(SearchServiceDeps{Repository: repo, Resolver: embeddingResolver, Logger: logger})

	_, err := svc.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: "退款",
	})
	if !errors.Is(err, domainerrors.ErrGenerationNotReady) {
		t.Fatalf("err = %v", err)
	}
	if handler.countEvent("search.failed") != 1 {
		t.Fatalf("failed events = %d, want 1\n%s", handler.countEvent("search.failed"), handler.json())
	}
}
