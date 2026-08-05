package pipeline

import (
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// selectChunkingStrategies returns the deterministic strategy order for one
// parsed document. Structured documents prefer heading boundaries; plain text
// starts with heuristic boundaries and both paths fall back to recursive cuts.
func selectChunkingStrategies(
	manifest model.ParseManifest,
	config value.ChunkingConfig,
) []value.ChunkingStrategy {
	config = config.Normalize()
	switch config.Strategy {
	case value.ChunkingStrategyHeading:
		return []value.ChunkingStrategy{
			value.ChunkingStrategyHeading,
			value.ChunkingStrategyHeuristic,
			value.ChunkingStrategyRecursive,
		}
	case value.ChunkingStrategyHeuristic:
		return []value.ChunkingStrategy{
			value.ChunkingStrategyHeuristic,
			value.ChunkingStrategyRecursive,
		}
	case value.ChunkingStrategyRecursive:
		return []value.ChunkingStrategy{value.ChunkingStrategyRecursive}
	default:
		for _, block := range manifest.Blocks {
			if block.Kind == model.BlockKindHeading || len(block.HeadingPath) > 0 {
				return []value.ChunkingStrategy{
					value.ChunkingStrategyHeading,
					value.ChunkingStrategyHeuristic,
					value.ChunkingStrategyRecursive,
				}
			}
		}
		return []value.ChunkingStrategy{
			value.ChunkingStrategyHeuristic,
			value.ChunkingStrategyRecursive,
		}
	}
}
