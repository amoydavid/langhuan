package index

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// StageEntry pairs one rebuildable projection fact with its validated vector.
type StageEntry struct {
	Entry     *model.RetrievalEntry
	Embedding []float32
}

// SearchCandidate is one ranked branch hit before reciprocal-rank fusion.
type SearchCandidate struct {
	EntryID uuid.UUID
	Score   float64
}

// SearchRequest fixes one active Generation and both branch limits.
type SearchRequest struct {
	KnowledgeBaseID, GenerationID uuid.UUID
	Query                         string
	QueryEmbedding                []float32
	FTSConfig                     string
	Dimension                     int
	VectorTopK, KeywordTopK       int
}

// SearchEvidence resolves current display facts for one selected projection row.
type SearchEvidence struct {
	EntryID, ChunkID, ChunkRevisionID, DocumentID uuid.UUID
	DocumentKind                                  value.DocumentKind
	Content, DocumentName                         string
	// SearchContent 是命中的检索原始文本（FAQ 为问题集合，file/web 为片段正文）。
	// 仅用于排序（如 Rerank 文本构造），不进入 API DTO。
	SearchContent                          string
	SourceAnchor                           value.SourceAnchor
	Metadata                               map[string]any
	MatchedChunkID, MatchedChunkRevisionID uuid.UUID
	MatchedContent                         string
	MatchedSearchContent                   string
	MatchedSourceAnchor                    value.SourceAnchor
	MatchedRole                            value.ChunkRole
}

// SearchReader performs candidate and evidence reads on one Workspace-bound transaction.
type SearchReader interface {
	GetActiveGeneration(context.Context, uuid.UUID) (*model.IndexGeneration, error)
	VectorCandidates(context.Context, SearchRequest) ([]SearchCandidate, error)
	KeywordCandidates(context.Context, SearchRequest) ([]SearchCandidate, error)
	LoadEvidence(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) ([]SearchEvidence, error)
}

// SearchRepository enters the tenant transaction used by search reads.
type SearchRepository interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, SearchReader) error) error
}

// Source is one ready ChunkSet with each Chunk's active immutable revision.
type Source struct {
	ChunkSet  *model.DocumentChunkSet
	Chunks    []*model.Chunk
	Revisions []*model.ChunkRevision
}

// SourceRepository loads deterministic ready indexing input inside one Workspace.
type SourceRepository interface {
	GetReadyIndexSource(context.Context, uuid.UUID, uuid.UUID) (*Source, error)
}

// RetrievalIndex persists FTS and vector data into one Generation's staging projection.
type RetrievalIndex interface {
	StageBatch(
		ctx context.Context,
		workspaceID uuid.UUID,
		ftsConfig string,
		dimension int,
		entries []StageEntry,
	) error
}
