package embedding

import (
	"context"
	"fmt"
	"math"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// EmbedStringsFunc 隔离 Eino 的具体 Embedder 类型和 option 类型。
type EmbedStringsFunc func(context.Context, []string) ([][]float64, error)

type validatedClient struct {
	embedStrings EmbedStringsFunc
	dimension    int
	batchSize    int
}

// NewValidatedClient 创建负责 batching、转换和响应验证的琅嬛 EmbeddingClient。
func NewValidatedClient(embedStrings EmbedStringsFunc, dimension, batchSize int) embeddingport.EmbeddingClient {
	if batchSize <= 0 {
		batchSize = 1
	}
	return &validatedClient{embedStrings: embedStrings, dimension: dimension, batchSize: batchSize}
}

func (c *validatedClient) Dimension() int {
	return c.dimension
}

func (c *validatedClient) Embed(ctx context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	if len(input.Texts) == 0 {
		return &embeddingport.EmbedResult{Vectors: make([][]float32, 0)}, nil
	}
	raw := make([][]float64, 0, len(input.Texts))
	for start := 0; start < len(input.Texts); start += c.batchSize {
		end := min(start+c.batchSize, len(input.Texts))
		batch, err := c.embedStrings(ctx, input.Texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("Embedding Provider 请求失败: %w", err)
		}
		raw = append(raw, batch...)
	}
	if len(raw) != len(input.Texts) {
		return nil, fmt.Errorf("%w: 向量数量 %d 与输入数量 %d 不一致", domainerrors.ErrInvalidEmbeddingResponse, len(raw), len(input.Texts))
	}

	actualDimension := -1
	for _, vector := range raw {
		if len(vector) == 0 {
			return nil, fmt.Errorf("%w: 返回了空向量", domainerrors.ErrInvalidEmbeddingResponse)
		}
		if actualDimension == -1 {
			actualDimension = len(vector)
		} else if len(vector) != actualDimension {
			return nil, fmt.Errorf("%w: 返回向量维度不一致", domainerrors.ErrInvalidEmbeddingResponse)
		}
	}
	if actualDimension != c.dimension {
		return nil, fmt.Errorf("%w: 实际 %d，声明 %d", domainerrors.ErrDimensionMismatch, actualDimension, c.dimension)
	}

	vectors := make([][]float32, len(raw))
	for i, vector := range raw {
		vectors[i] = make([]float32, len(vector))
		for j, number := range vector {
			if math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > math.MaxFloat32 {
				return nil, fmt.Errorf("%w: 向量包含非有限或越界数值", domainerrors.ErrInvalidEmbeddingResponse)
			}
			vectors[i][j] = float32(number)
		}
	}
	return &embeddingport.EmbedResult{Vectors: vectors}, nil
}
