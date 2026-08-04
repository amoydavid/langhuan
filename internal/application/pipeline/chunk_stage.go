package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"io"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ChunkStage materializes one deterministic standard ChunkSet.
type ChunkStage struct {
	revisions   DocumentRevisionRepository
	documents   RevisionDocumentGetter
	generations IndexGenerationGetter
	chunkSets   ChunkSetRepository
	chunker     Chunker
}

// NewChunkStage creates a revision- and generation-scoped chunk stage.
func NewChunkStage(
	revisions DocumentRevisionRepository,
	documents RevisionDocumentGetter,
	generations IndexGenerationGetter,
	chunkSets ChunkSetRepository,
	chunker Chunker,
) ChunkStage {
	return ChunkStage{
		revisions: revisions, documents: documents, generations: generations,
		chunkSets: chunkSets, chunker: chunker,
	}
}

// Run creates or reuses the standard ChunkSet for the requested revision/configuration.
func (s ChunkStage) Run(ctx context.Context, workspaceID, revisionID, generationID uuid.UUID) (uuid.UUID, error) {
	revision, err := s.revisions.Get(ctx, workspaceID, revisionID)
	if err != nil {
		return uuid.Nil, err
	}
	if revision.Kind == value.DocumentKindFAQ {
		return uuid.Nil, fmt.Errorf("%w: FAQ Revision 必须使用固定 FAQ 分块策略", domainerrors.ErrValidation)
	}
	if revision.Status != value.DocumentRevisionReady || revision.ParseManifest == nil {
		return uuid.Nil, fmt.Errorf("%w: DocumentRevision 尚未解析完成", domainerrors.ErrValidation)
	}
	document, err := s.documents.Get(ctx, workspaceID, revision.DocumentID)
	if err != nil {
		return uuid.Nil, err
	}
	generation, err := s.generations.Get(ctx, workspaceID, generationID)
	if err != nil {
		return uuid.Nil, err
	}
	if document.KnowledgeBaseID != revision.KnowledgeBaseID || document.Kind != revision.Kind ||
		generation.KnowledgeBaseID != revision.KnowledgeBaseID {
		return uuid.Nil, fmt.Errorf("%w: Revision/Document/Generation lineage 不一致", domainerrors.ErrValidation)
	}
	if generation.ChunkerVersion != CurrentStandardChunkerVersion {
		return uuid.Nil, fmt.Errorf(
			"%w: 不支持 standard chunker version %d",
			domainerrors.ErrValidation,
			generation.ChunkerVersion,
		)
	}
	config, err := decodeChunkingConfig(generation.ChunkingConfig)
	if err != nil {
		return uuid.Nil, err
	}
	configMap := map[string]any{"chunk_size": config.ChunkSize, "chunk_overlap": config.ChunkOverlap}
	configHash, err := standardChunkConfigHash(generation.ChunkerVersion, configMap)
	if err != nil {
		return uuid.Nil, err
	}
	candidate := &model.DocumentChunkSet{
		ID: id.New(), WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		DocumentID: revision.DocumentID, DocumentRevisionID: revision.ID,
		Strategy: value.ChunkStrategyStandard, ChunkerVersion: generation.ChunkerVersion,
		ChunkingConfig: configMap, ConfigHash: configHash, Status: value.ChunkSetBuilding,
		CreatedAt: time.Now().UTC(),
	}
	chunkSet, err := s.chunkSets.GetOrCreate(ctx, workspaceID, candidate)
	if err != nil {
		return uuid.Nil, err
	}
	if chunkSet.Status == value.ChunkSetReady {
		return chunkSet.ID, nil
	}
	chunks, revisions, err := s.chunker.Chunk(ChunkInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		DocumentID: revision.DocumentID, DocumentRevisionID: revision.ID,
		ChunkSetID: chunkSet.ID, Kind: revision.Kind, Title: document.Title,
		Markdown: revision.NormalizedMarkdown, Manifest: *revision.ParseManifest,
	}, config)
	if err != nil {
		return uuid.Nil, err
	}
	completed, err := s.chunkSets.Complete(ctx, workspaceID, chunkSet.ID, chunks, revisions)
	if err != nil {
		return uuid.Nil, err
	}
	return completed.ID, nil
}

func decodeChunkingConfig(raw map[string]any) (value.ChunkingConfig, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return value.ChunkingConfig{}, fmt.Errorf("编码 ChunkingConfig 失败: %w", err)
	}
	var encodedConfig struct {
		ChunkSize    int `json:"chunk_size"`
		ChunkOverlap int `json:"chunk_overlap"`
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&encodedConfig); err != nil {
		return value.ChunkingConfig{}, fmt.Errorf("%w: 解码 ChunkingConfig 失败: %v", domainerrors.ErrValidation, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value.ChunkingConfig{}, fmt.Errorf("%w: ChunkingConfig 包含多余 JSON 值", domainerrors.ErrValidation)
	}
	config := value.ChunkingConfig{
		ChunkSize: encodedConfig.ChunkSize, ChunkOverlap: encodedConfig.ChunkOverlap,
	}
	if err := config.Validate(); err != nil {
		return value.ChunkingConfig{}, err
	}
	return config, nil
}

func standardChunkConfigHash(chunkerVersion int, config map[string]any) (string, error) {
	encoded, err := json.Marshal(struct {
		Strategy       value.ChunkStrategy `json:"strategy"`
		ChunkerVersion int                 `json:"chunker_version"`
		Config         map[string]any      `json:"chunking_config"`
	}{Strategy: value.ChunkStrategyStandard, ChunkerVersion: chunkerVersion, Config: config})
	if err != nil {
		return "", fmt.Errorf("编码标准分块配置指纹失败: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
