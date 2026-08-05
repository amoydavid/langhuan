package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestChunkValidateLineage(t *testing.T) {
	parentID := uuid.New()
	tests := []struct {
		name    string
		chunk   Chunk
		wantErr bool
	}{
		{name: "parent has no parent", chunk: Chunk{Role: value.ChunkRoleParent}},
		{name: "child has parent", chunk: Chunk{Role: value.ChunkRoleChild, ParentChunkID: &parentID}},
		{name: "flat has no parent", chunk: Chunk{Role: value.ChunkRoleFlat}},
		{name: "child needs parent", chunk: Chunk{Role: value.ChunkRoleChild}, wantErr: true},
		{name: "parent cannot have parent", chunk: Chunk{Role: value.ChunkRoleParent, ParentChunkID: &parentID}, wantErr: true},
		{name: "flat cannot have parent", chunk: Chunk{Role: value.ChunkRoleFlat, ParentChunkID: &parentID}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.chunk.ValidateLineage()
			if test.wantErr && !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("error = %v, want validation", err)
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}
