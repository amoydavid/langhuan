package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// StandardChunkerVersion identifies the current deterministic standard chunking contract.
const StandardChunkerVersion = 2

type ChunkingConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

func DefaultChunkingConfig() ChunkingConfig {
	return ChunkingConfig{ChunkSize: 512, ChunkOverlap: 80}
}

func (c ChunkingConfig) Normalize() ChunkingConfig {
	if c.ChunkSize <= 0 {
		c.ChunkSize = 512
	}
	if c.ChunkOverlap < 0 {
		c.ChunkOverlap = 80
	}
	return c
}

func (c ChunkingConfig) Validate() error {
	if c.ChunkSize <= 0 {
		return fmt.Errorf("%w: chunk_size 必须大于 0", domainerrors.ErrValidation)
	}
	if c.ChunkOverlap < 0 {
		return fmt.Errorf("%w: chunk_overlap 不能小于 0", domainerrors.ErrValidation)
	}
	if c.ChunkOverlap >= c.ChunkSize {
		return fmt.Errorf("%w: chunk_overlap 必须小于 chunk_size", domainerrors.ErrValidation)
	}
	return nil
}
