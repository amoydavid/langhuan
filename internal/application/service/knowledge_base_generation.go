package service

import (
	"fmt"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

const (
	minRetrievalTopK = 1
	maxCandidateTopK = 1000
	maxFinalTopK     = 50
)

// RetrievalConfig is the typed immutable retrieval snapshot stored in a Generation.
type RetrievalConfig struct {
	FTSConfig   string `json:"fts_config"`
	VectorTopK  int    `json:"vector_top_k"`
	KeywordTopK int    `json:"keyword_top_k"`
	FinalTopK   int    `json:"final_top_k"`
	RRFK        int    `json:"rrf_k"`
}

// DefaultRetrievalConfig returns the hybrid-retrieval defaults.
// fts_config 默认使用 zhparser（中文全文检索，见迁移 000011）；simple 分词对
// 中文不做词边界切分，会导致 FTS 路在中文文档上基本失效。
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{FTSConfig: "zhparser", VectorTopK: 30, KeywordTopK: 30, FinalTopK: 10, RRFK: 60}
}

func buildInitialKnowledgeBaseState(kb *model.KnowledgeBase, resolved *model.ResolvedModel) (*model.FileTreeNode, *model.IndexGeneration, error) {
	if kb == nil || resolved == nil || resolved.Model == nil || resolved.Provider == nil || resolved.Model.Dimensions == nil {
		return nil, nil, fmt.Errorf("构造初始索引代次失败: %w", domainerrors.ErrValidation)
	}
	root, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID, NodeType: value.FileTreeNodeRoot,
	})
	if err != nil {
		return nil, nil, err
	}
	chunkingConfig := map[string]any{
		"chunk_size": kb.ChunkingConfig.ChunkSize, "chunk_overlap": kb.ChunkingConfig.ChunkOverlap,
	}
	retrieval := DefaultRetrievalConfig()
	retrievalConfig := map[string]any{
		"fts_config": retrieval.FTSConfig, "vector_top_k": retrieval.VectorTopK,
		"keyword_top_k": retrieval.KeywordTopK, "final_top_k": retrieval.FinalTopK, "rrf_k": retrieval.RRFK,
	}
	modelConfigHash, err := CanonicalConfigHash(map[string]any{
		"provider": resolved.Provider.Provider, "provider_config": resolved.Provider.Config,
		"model_name": resolved.Model.ModelName, "dimensions": *resolved.Model.Dimensions,
		"parameters": resolved.Model.Parameters,
	})
	if err != nil {
		return nil, nil, err
	}
	configHash, err := CanonicalConfigHash(map[string]any{
		"model_config_hash": modelConfigHash, "chunker_version": value.StandardChunkerVersion,
		"chunking_config": chunkingConfig, "retrieval_config": retrievalConfig,
	})
	if err != nil {
		return nil, nil, err
	}
	generation, err := model.NewIndexGeneration(model.NewIndexGenerationInput{
		WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
		EmbeddingModelID: resolved.Model.ID, ProviderID: resolved.Provider.ID,
		ModelName: resolved.Model.ModelName, EmbeddingDimension: *resolved.Model.Dimensions,
		ModelConfigHash: modelConfigHash, ChunkerVersion: value.StandardChunkerVersion,
		ChunkingConfig: chunkingConfig, RetrievalConfig: retrievalConfig, ConfigHash: configHash,
		Status: value.IndexGenerationReady, ManualEditDisposition: value.ManualEditNotApplicable,
	})
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	generation.ReadyAt = &now
	kb.FileTreeRootID = root.ID
	kb.ActiveIndexGenerationID = &generation.ID
	kb.ContentVersion = 0
	return root, generation, nil
}
