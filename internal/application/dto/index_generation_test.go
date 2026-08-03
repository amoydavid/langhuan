package dto

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestIndexGenerationOmitsFailedCount(t *testing.T) {
	if _, exists := reflect.TypeOf(IndexGeneration{}).FieldByName("FailedCount"); exists {
		t.Fatal("IndexGeneration DTO still exposes FailedCount")
	}

	payload, err := json.Marshal(IndexGenerationFromModel(&model.IndexGeneration{}))
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	if _, exists := response["failed_count"]; exists {
		t.Fatal("IndexGeneration JSON still exposes failed_count")
	}
}

func TestIndexGenerationProvidesReadableDisplayLabel(t *testing.T) {
	id := "5de1f306-118b-4c2e-86f8-acde3cb6bdb4"
	generation := &model.IndexGeneration{
		ModelName: "text-embedding-v4",
		Status:    value.IndexGenerationReady,
		CreatedAt: time.Date(2026, time.August, 1, 11, 8, 0, 0, time.UTC),
	}
	generation.ID = uuid.MustParse(id)

	result := IndexGenerationFromModel(generation)
	if result == nil {
		t.Fatal("IndexGenerationFromModel() = nil")
	}
	if !strings.Contains(result.DisplayLabel, "2026-08-01 11:08") ||
		!strings.Contains(result.DisplayLabel, "text-embedding-v4") ||
		!strings.Contains(result.DisplayLabel, "已就绪") {
		t.Fatalf("DisplayLabel = %q, want readable time, model and status", result.DisplayLabel)
	}
	if strings.Contains(result.DisplayLabel, id) {
		t.Fatalf("DisplayLabel = %q, must not expose generation UUID", result.DisplayLabel)
	}
}
