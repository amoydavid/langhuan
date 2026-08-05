package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestKnowledgeBaseCreateAdapterPreservesExplicitParentChildDisable(t *testing.T) {
	creator := &knowledgeBaseCreatorFake{}
	adapter := NewMCPKnowledgeBaseService(creator)
	parentChild := false
	parentSize, childSize, chunkSize, overlap := 2048, 256, 512, 64

	_, err := adapter.Create(context.Background(), MCPCreateKnowledgeBaseInput{
		WorkspaceID: uuid.New(), Name: "手册", EmbeddingModelID: uuid.New(),
		Strategy:          &[]value.ChunkingStrategy{value.ChunkingStrategyRecursive}[0],
		EnableParentChild: &parentChild,
		ParentChunkSize:   &parentSize,
		ChildChunkSize:    &childSize,
		ChunkSize:         &chunkSize,
		ChunkOverlap:      &overlap,
	})
	require.NoError(t, err)
	require.NotNil(t, creator.input.ChunkingConfig)
	require.Equal(t, value.ChunkingStrategyRecursive, creator.input.ChunkingConfig.Strategy)
	require.False(t, creator.input.ChunkingConfig.EnableParentChild)
	require.Equal(t, 512, creator.input.ChunkingConfig.ChunkSize)
	require.Equal(t, 64, creator.input.ChunkingConfig.ChunkOverlap)
}

type knowledgeBaseCreatorFake struct {
	input service.CreateKnowledgeBaseInput
}

func (f *knowledgeBaseCreatorFake) Create(_ context.Context, input service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	f.input = input
	return &dto.KnowledgeBase{}, nil
}
