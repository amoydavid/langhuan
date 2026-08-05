package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// recordingRerankClientForSearch 记录调用输入并按固定顺序返回分数。
type recordingRerankClientForSearch struct {
	calls  int
	input  rerankport.RerankInput
	scores []float64
	err    error
}

func (c *recordingRerankClientForSearch) Rerank(_ context.Context, input rerankport.RerankInput) (*rerankport.RerankResult, error) {
	c.calls++
	c.input = input
	if c.err != nil {
		return nil, c.err
	}
	items := make([]rerankport.RerankItem, 0, len(input.Documents))
	for index, document := range input.Documents {
		score := 0.1
		if index < len(c.scores) {
			score = c.scores[index]
		}
		items = append(items, rerankport.RerankItem{DocumentID: document.ID, Score: score})
	}
	return &rerankport.RerankResult{Items: items}, nil
}

type stubRerankResolver struct {
	client *ResolvedRerankClient
	err    error
}

func (r *stubRerankResolver) Resolve(context.Context, uuid.UUID, uuid.UUID) (*ResolvedRerankClient, error) {
	return r.client, r.err
}

func TestBuildRerankDocumentsUsesSearchContent(t *testing.T) {
	t.Parallel()
	result := &dto.SearchResult{ChunkID: uuid.New(), Content: "答案正文"}
	rankables := []*rankableSearchResult{
		{Result: result, MatchedSearchContent: []string{"问题一\n问题二"}},
	}
	docs, err := buildRerankDocuments(rankables, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Text != "问题一\n问题二" {
		t.Fatalf("docs = %#v", docs)
	}
	if docs[0].ID != "candidate-000001" {
		t.Fatalf("opaque id = %q", docs[0].ID)
	}
}

func TestBuildRerankDocumentsDedupesAndTruncates(t *testing.T) {
	t.Parallel()
	long := make([]rune, 9000)
	for i := range long {
		long[i] = 'x'
	}
	rankables := []*rankableSearchResult{
		{Result: &dto.SearchResult{ChunkID: uuid.New()}, MatchedSearchContent: []string{"a", "a", string(long)}},
	}
	docs, err := buildRerankDocuments(rankables, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(docs[0].Text)) != 8192 {
		t.Fatalf("truncated len = %d", len([]rune(docs[0].Text)))
	}
}

func TestApplyRerankStableOrderByScoreThenRRF(t *testing.T) {
	t.Parallel()
	high := 0.95
	low := 0.2
	resultHigh := &dto.SearchResult{ChunkID: uuid.New(), Score: 0.03}
	resultLow := &dto.SearchResult{ChunkID: uuid.New(), Score: 0.5}
	rankables := []*rankableSearchResult{
		{Result: resultLow, MatchedSearchContent: []string{"b"}},
		{Result: resultHigh, MatchedSearchContent: []string{"a"}},
	}
	client := &ResolvedRerankClient{Client: &recordingRerankClientForSearch{scores: []float64{low, high}}, MaxDocumentChars: 8192}
	ranked, stage, err := applyRerank(context.Background(), client, rankables, 50, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if stage != value.RankingStageRerank {
		t.Fatalf("stage = %s", stage)
	}
	if ranked[0].Result != resultHigh || *ranked[0].RerankScore != high {
		t.Fatalf("ranked[0] = %#v", ranked[0])
	}
	if ranked[0].Result.RerankScore == nil || *ranked[0].Result.RerankScore != high {
		t.Fatalf("result rerank score not written = %#v", ranked[0].Result)
	}
}

func TestIsRerankRecoverable(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		domainerrors.ErrRerankUnavailable, domainerrors.ErrRerankRateLimited,
		domainerrors.ErrRequestTimeout, domainerrors.ErrInvalidRerankResponse,
	} {
		if !isRerankRecoverable(err) {
			t.Fatalf("%v should be recoverable", err)
		}
	}
	// context cancel / snapshot mismatch 不属于可回退错误。
	if isRerankRecoverable(domainerrors.ErrRerankSnapshotMismatch) {
		t.Fatal("snapshot mismatch should not be recoverable")
	}
	if isRerankRecoverable(errors.New("random")) {
		t.Fatal("random error should not be recoverable")
	}
}
