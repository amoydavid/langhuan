package index

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

type testRetrievalIndex struct{}

func (testRetrievalIndex) StageBatch(context.Context, uuid.UUID, string, int, []StageEntry) error {
	return nil
}

type testSourceRepository struct{}

func (testSourceRepository) GetReadyIndexSource(context.Context, uuid.UUID, uuid.UUID) (*Source, error) {
	return &Source{}, nil
}

type testSearchRepository struct{}

func (testSearchRepository) WithinWorkspace(
	ctx context.Context,
	_ uuid.UUID,
	fn func(context.Context, SearchReader) error,
) error {
	return fn(ctx, testSearchReader{})
}

type testSearchReader struct{}

func (testSearchReader) GetActiveGeneration(context.Context, uuid.UUID) (*model.IndexGeneration, error) {
	return &model.IndexGeneration{}, nil
}

func (testSearchReader) VectorCandidates(context.Context, SearchRequest) ([]SearchCandidate, error) {
	return nil, nil
}

func (testSearchReader) KeywordCandidates(context.Context, SearchRequest) ([]SearchCandidate, error) {
	return nil, nil
}

func (testSearchReader) LoadEvidence(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) ([]SearchEvidence, error) {
	return nil, nil
}

var _ RetrievalIndex = testRetrievalIndex{}
var _ SourceRepository = testSourceRepository{}
var _ SearchRepository = testSearchRepository{}
