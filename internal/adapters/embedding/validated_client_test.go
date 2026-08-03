package embedding

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

func TestValidatedClientBatchesAndConvertsVectors(t *testing.T) {
	t.Parallel()

	var batches [][]string
	client := NewValidatedClient(func(_ context.Context, texts []string) ([][]float64, error) {
		batches = append(batches, append([]string(nil), texts...))
		result := make([][]float64, len(texts))
		for i := range texts {
			result[i] = []float64{1.25, -2.5}
		}
		return result, nil
	}, 2, 2)

	got, err := client.Embed(context.Background(), embeddingport.EmbedInput{Texts: []string{"a", "b", "c", "d", "e"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(batches, [][]string{{"a", "b"}, {"c", "d"}, {"e"}}) {
		t.Fatalf("batches = %#v", batches)
	}
	want := [][]float32{{1.25, -2.5}, {1.25, -2.5}, {1.25, -2.5}, {1.25, -2.5}, {1.25, -2.5}}
	if !reflect.DeepEqual(got.Vectors, want) {
		t.Fatalf("vectors = %#v", got.Vectors)
	}
	if client.Dimension() != 2 {
		t.Fatalf("dimension = %d", client.Dimension())
	}
}

func TestValidatedClientRejectsInvalidEmbeddingResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		vectors   [][]float64
		dimension int
		wantError error
	}{
		{name: "wrong count", vectors: nil, dimension: 2, wantError: domainerrors.ErrInvalidEmbeddingResponse},
		{name: "empty vector", vectors: [][]float64{{}}, dimension: 2, wantError: domainerrors.ErrInvalidEmbeddingResponse},
		{name: "dimension mismatch", vectors: [][]float64{{1}}, dimension: 2, wantError: domainerrors.ErrDimensionMismatch},
		{name: "nan", vectors: [][]float64{{1, math.NaN()}}, dimension: 2, wantError: domainerrors.ErrInvalidEmbeddingResponse},
		{name: "positive infinity", vectors: [][]float64{{1, math.Inf(1)}}, dimension: 2, wantError: domainerrors.ErrInvalidEmbeddingResponse},
		{name: "float32 overflow", vectors: [][]float64{{1, math.MaxFloat64}}, dimension: 2, wantError: domainerrors.ErrInvalidEmbeddingResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewValidatedClient(func(context.Context, []string) ([][]float64, error) {
				return tt.vectors, nil
			}, tt.dimension, 32)
			_, err := client.Embed(context.Background(), embeddingport.EmbedInput{Texts: []string{"text"}})
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatedClientRejectsInconsistentDimensionsAndPropagatesContext(t *testing.T) {
	t.Parallel()

	client := NewValidatedClient(func(ctx context.Context, _ []string) ([][]float64, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return [][]float64{{1, 2}, {1, 2, 3}}, nil
	}, 2, 32)
	_, err := client.Embed(context.Background(), embeddingport.EmbedInput{Texts: []string{"one", "two"}})
	if !errors.Is(err, domainerrors.ErrInvalidEmbeddingResponse) {
		t.Fatalf("inconsistent dimension error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Embed(cancelled, embeddingport.EmbedInput{Texts: []string{"one"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}

func TestValidatedClientHandlesEmptyInputWithoutCallingProvider(t *testing.T) {
	t.Parallel()

	called := false
	client := NewValidatedClient(func(context.Context, []string) ([][]float64, error) {
		called = true
		return nil, nil
	}, 1024, 32)
	got, err := client.Embed(context.Background(), embeddingport.EmbedInput{})
	if err != nil {
		t.Fatal(err)
	}
	if called || len(got.Vectors) != 0 {
		t.Fatalf("called = %v, vectors = %#v", called, got.Vectors)
	}
}
