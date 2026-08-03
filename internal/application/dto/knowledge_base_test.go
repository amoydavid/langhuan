package dto

import (
	"encoding/json"
	"testing"
)

func TestKnowledgeBaseChunkingConfigUsesAPIFieldNames(t *testing.T) {
	payload, err := json.Marshal(KnowledgeBase{
		ChunkingConfig: ChunkingConfig{ChunkSize: 512, ChunkOverlap: 80},
	})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	chunking, ok := response["chunking_config"].(map[string]any)
	if !ok {
		t.Fatalf("chunking_config = %#v", response["chunking_config"])
	}
	if chunking["chunk_size"] != float64(512) || chunking["chunk_overlap"] != float64(80) {
		t.Fatalf("chunking_config = %#v", chunking)
	}
}
