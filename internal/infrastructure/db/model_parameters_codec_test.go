package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestModelRowRoundTripPreservesNullableDimensionsAndParameters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	dimension, actorID := 1024, uuid.New()
	want := &model.Model{
		ID:          uuid.New(),
		ProviderID:  uuid.New(),
		Name:        "embedding-v4",
		DisplayName: "Embedding V4",
		Description: "desc",
		Type:        value.ModelTypeEmbedding,
		ModelName:   "text-embedding-v4",
		Dimensions:  &dimension,
		Parameters:  map[string]any{"batch_size": float64(32)},
		Status:      value.ModelStatusActive,
		CreatedBy:   &actorID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	row, err := modelToRow(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := modelFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.ProviderID != want.ProviderID || got.Dimensions == nil || *got.Dimensions != dimension {
		t.Fatalf("identity/dimensions = %#v", got)
	}
	if !reflect.DeepEqual(got.Parameters, want.Parameters) {
		t.Fatalf("parameters = %#v", got.Parameters)
	}
	row.Parameters["batch_size"] = float64(1)
	if got.Parameters["batch_size"] != float64(32) {
		t.Fatal("domain model aliases mutable row parameters")
	}
}

func TestModelRowSupportsFutureTypesWithNilDimensions(t *testing.T) {
	t.Parallel()

	row := ModelRow{ID: uuid.New(), ProviderID: uuid.New(), Name: "chat", Type: "llm", ModelName: "chat-model", Parameters: JSONMap{}, Status: "active"}
	got, err := modelFromRow(&row)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != value.ModelTypeLLM || got.Dimensions != nil {
		t.Fatalf("model = %#v", got)
	}
}

func TestModelFromRowRejectsUnknownTypeAndStatus(t *testing.T) {
	t.Parallel()

	dimension := 1024
	base := ModelRow{ID: uuid.New(), ProviderID: uuid.New(), Name: "model", Type: "embedding", ModelName: "model", Dimensions: &dimension, Parameters: JSONMap{}, Status: "active"}
	badType := base
	badType.Type = "asr"
	if _, err := modelFromRow(&badType); err == nil {
		t.Fatal("expected unknown type error")
	}
	badStatus := base
	badStatus.Status = "deleted"
	if _, err := modelFromRow(&badStatus); err == nil {
		t.Fatal("expected unknown status error")
	}
}
