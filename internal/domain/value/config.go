package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// StandardChunkerVersion identifies the current deterministic standard chunking contract.
const StandardChunkerVersion = 3

// ChunkingStrategy selects the deterministic boundary strategy for File and Web documents.
type ChunkingStrategy string

const (
	ChunkingStrategyAuto      ChunkingStrategy = "auto"
	ChunkingStrategyHeading   ChunkingStrategy = "heading"
	ChunkingStrategyHeuristic ChunkingStrategy = "heuristic"
	ChunkingStrategyRecursive ChunkingStrategy = "recursive"
)

type ChunkingConfig struct {
	ChunkSize         int
	ChunkOverlap      int
	Strategy          ChunkingStrategy
	EnableParentChild bool
	ParentChunkSize   int
	ChildChunkSize    int
}

func DefaultChunkingConfig() ChunkingConfig {
	return ChunkingConfig{
		ChunkSize: 512, ChunkOverlap: 80,
		Strategy: ChunkingStrategyAuto, EnableParentChild: true,
		ParentChunkSize: 4096, ChildChunkSize: 384,
	}
}

func (c ChunkingConfig) Normalize() ChunkingConfig {
	if c.Strategy == "" {
		c.Strategy = ChunkingStrategyAuto
	}
	if c.ChunkSize <= 0 {
		c.ChunkSize = 512
	}
	if c.ChunkOverlap < 0 {
		c.ChunkOverlap = 80
	}
	if c.ParentChunkSize <= 0 {
		c.ParentChunkSize = 4096
	}
	if c.ChildChunkSize <= 0 {
		c.ChildChunkSize = 384
	}
	return c
}

func (c ChunkingConfig) Validate() error {
	c = c.Normalize()
	switch c.Strategy {
	case ChunkingStrategyAuto, ChunkingStrategyHeading, ChunkingStrategyHeuristic, ChunkingStrategyRecursive:
	default:
		return fmt.Errorf("%w: strategy 无效", domainerrors.ErrValidation)
	}
	if c.EnableParentChild {
		if c.ParentChunkSize < 512 || c.ParentChunkSize > 8192 {
			return fmt.Errorf("%w: parent_chunk_size 必须在 512 到 8192 之间", domainerrors.ErrValidation)
		}
		if c.ChildChunkSize < 64 || c.ChildChunkSize > 2048 {
			return fmt.Errorf("%w: child_chunk_size 必须在 64 到 2048 之间", domainerrors.ErrValidation)
		}
		if c.ChildChunkSize > c.ParentChunkSize {
			return fmt.Errorf("%w: child_chunk_size 不能大于 parent_chunk_size", domainerrors.ErrValidation)
		}
		if c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ParentChunkSize {
			return fmt.Errorf("%w: chunk_overlap 必须大于等于 0 且小于 parent_chunk_size", domainerrors.ErrValidation)
		}
		return nil
	}
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
