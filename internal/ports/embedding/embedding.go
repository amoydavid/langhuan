package embedding

import "context"

type EmbedInput struct {
	Texts []string
}

type EmbedResult struct {
	Vectors [][]float32
}

type EmbeddingClient interface {
	Embed(ctx context.Context, input EmbedInput) (*EmbedResult, error)
	Dimension() int
}
