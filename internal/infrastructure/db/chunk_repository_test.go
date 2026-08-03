package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestSourceAnchorJSONMapRoundTrip(t *testing.T) {
	one, two, three := 1, 2, 3
	want := value.SourceAnchor{
		SourceType: "xlsx", Sheet: "数据", HeaderRow: &one,
		RowStart: &two, RowEnd: &three, ColumnStart: &one, ColumnEnd: &three,
	}
	got, err := sourceAnchorFromJSONMap(sourceAnchorToJSONMap(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source anchor = %#v, want %#v", got, want)
	}
}

func TestChunkRowMappingPreservesJSONAndIdentity(t *testing.T) {
	now := time.Date(2026, 6, 17, 13, 0, 0, 0, time.UTC)
	activeRevisionID := uuid.New()
	chunk := &model.Chunk{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		DocumentID: uuid.New(), DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(),
		Sequence: 0, SourceContent: "# A", ActiveRevisionID: &activeRevisionID,
		SourceAnchor: value.SourceAnchor{SourceType: "stub"}, Metadata: map[string]any{"source": "unit"},
		CreatedAt: now,
	}

	row, err := chunkToRow(chunk)
	if err != nil {
		t.Fatal(err)
	}
	got, err := chunkFromRow(row)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != chunk.ID || got.WorkspaceID != chunk.WorkspaceID || got.DocumentID != chunk.DocumentID || got.CreatedAt != now {
		t.Fatalf("identity/time not preserved: %#v", got)
	}
	if got.Sequence != chunk.Sequence || got.SourceContent != chunk.SourceContent || got.ActiveRevisionID == nil || *got.ActiveRevisionID != activeRevisionID {
		t.Fatalf("content fields not preserved: %#v", got)
	}
	if !reflect.DeepEqual(got.SourceAnchor, chunk.SourceAnchor) {
		t.Fatalf("source_anchor = %#v", got.SourceAnchor)
	}
	if !reflect.DeepEqual(got.Metadata, chunk.Metadata) {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
}
