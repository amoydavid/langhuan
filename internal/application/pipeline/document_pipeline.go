package pipeline

import (
	"context"
	"io"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// DocumentRevisionRepository persists immutable parse results.
type DocumentRevisionRepository interface {
	Get(ctx context.Context, workspaceID, revisionID uuid.UUID) (*model.DocumentRevision, error)
	CompleteParse(
		ctx context.Context,
		workspaceID, revisionID uuid.UUID,
		markdown string,
		manifest model.ParseManifest,
	) error
}

// RevisionDocumentGetter reads stable Document identity for a revision.
type RevisionDocumentGetter interface {
	Get(ctx context.Context, workspaceID, documentID uuid.UUID) (*model.Document, error)
}

// IndexGenerationGetter reads the immutable chunking snapshot requested by a job.
type IndexGenerationGetter interface {
	Get(ctx context.Context, workspaceID, generationID uuid.UUID) (*model.IndexGeneration, error)
}

// ChunkSetRepository owns idempotent ChunkSet creation and atomic completion.
type ChunkSetRepository interface {
	GetOrCreate(
		ctx context.Context,
		workspaceID uuid.UUID,
		candidate *model.DocumentChunkSet,
	) (*model.DocumentChunkSet, error)
	Complete(
		ctx context.Context,
		workspaceID, chunkSetID uuid.UUID,
		chunks []*model.Chunk,
		revisions []*model.ChunkRevision,
	) (*model.DocumentChunkSet, error)
}

// RawDocumentReader opens persisted raw document content for parsing.
type RawDocumentReader interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// DocumentPipelineDeps contains the revision-scoped pipeline dependencies.
type DocumentPipelineDeps struct {
	Documents         RevisionDocumentGetter
	Revisions         DocumentRevisionRepository
	Generations       IndexGenerationGetter
	ChunkSets         ChunkSetRepository
	FAQRevisions      FAQRevisionGetter
	IndexSources      indexport.SourceRepository
	EmbeddingResolver appservice.EmbeddingClientResolver
	RetrievalIndex    indexport.RetrievalIndex
	Publisher         appservice.DocumentPublishStore
	Parser            parserport.DocumentParser
	RawStore          RawDocumentReader
	MaxFileSizeBytes  int64
}

// DocumentPipeline coordinates parse and standard chunk stages.
type DocumentPipeline struct {
	parse     ParseStage
	chunk     ChunkStage
	faq       FAQChunkStage
	index     IndexStage
	revisions DocumentRevisionRepository
}

// NewDocumentPipeline creates the immutable-revision pipeline.
func NewDocumentPipeline(deps DocumentPipelineDeps) *DocumentPipeline {
	return &DocumentPipeline{
		parse: NewParseStage(deps.Revisions, deps.Documents, deps.RawStore, deps.Parser, deps.MaxFileSizeBytes),
		chunk: NewChunkStage(deps.Revisions, deps.Documents, deps.Generations, deps.ChunkSets, NewChunker()),
		faq:   NewFAQChunkStage(deps.FAQRevisions, deps.ChunkSets),
		index: NewIndexStage(IndexStageDeps{
			Generations: deps.Generations, Sources: deps.IndexSources,
			Resolver: deps.EmbeddingResolver, Index: deps.RetrievalIndex, Publisher: deps.Publisher,
		}),
		revisions: deps.Revisions,
	}
}

// RunIndex stages and atomically publishes one ready ChunkSet into a Generation.
func (p *DocumentPipeline) RunIndex(
	ctx context.Context,
	workspaceID, generationID, chunkSetID uuid.UUID,
) ([]*model.RetrievalEntry, error) {
	return p.index.Run(ctx, workspaceID, generationID, chunkSetID)
}

// RunParse parses one File/Web DocumentRevision without publishing it.
func (p *DocumentPipeline) RunParse(ctx context.Context, workspaceID, revisionID uuid.UUID) error {
	_, err := p.parse.Run(ctx, workspaceID, revisionID)
	return err
}

// RunChunk builds or reuses one standard ChunkSet under an IndexGeneration snapshot.
func (p *DocumentPipeline) RunChunk(
	ctx context.Context,
	workspaceID, revisionID, generationID uuid.UUID,
) (uuid.UUID, error) {
	revision, err := p.revisions.Get(ctx, workspaceID, revisionID)
	if err != nil {
		return uuid.Nil, err
	}
	if revision.Kind == value.DocumentKindFAQ {
		return p.BuildFAQChunkSet(ctx, workspaceID, revisionID)
	}
	return p.chunk.Run(ctx, workspaceID, revisionID, generationID)
}

// BuildFAQChunkSet builds or reuses the fixed FAQ ChunkSet.
func (p *DocumentPipeline) BuildFAQChunkSet(ctx context.Context, workspaceID, revisionID uuid.UUID) (uuid.UUID, error) {
	return p.faq.Build(ctx, workspaceID, revisionID)
}
