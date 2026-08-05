package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// rankableSearchResult 携带 SearchResult 与用于构造重排文本的命中 search_content。
type rankableSearchResult struct {
	Result               *dto.SearchResult
	RerankScore          *float64
	MatchedSearchContent []string
}

// matchedSearchContentOf 返回命中 entry 的 search_content：
// 子块命中返回 MatchedSearchContent，flat 命中返回 SearchContent。
func matchedSearchContentOf(evidence indexport.SearchEvidence) string {
	if evidence.MatchedSearchContent != "" {
		return evidence.MatchedSearchContent
	}
	return evidence.SearchContent
}

// buildRankablesWithContent 构造带命中 search_content 的 rankable（单库/多库共用）。
// results 必须已按 RRF 稳定排序。
func buildRankablesWithContent(results []*dto.SearchResult, searchContentByChunk map[uuid.UUID][]string) []*rankableSearchResult {
	rankables := make([]*rankableSearchResult, len(results))
	for index, result := range results {
		rankables[index] = &rankableSearchResult{
			Result:               result,
			MatchedSearchContent: searchContentByChunk[result.ChunkID],
		}
	}
	return rankables
}

// applyRerank 在 parent grouping 之后执行一次重排：
// 取前 candidateTopK 个聚合结果构造文档，调用 Rerank，按 rerank DESC, RRF DESC 稳定排序。
// 返回重排后的 rankable（Result 字段已按新顺序排列，RerankScore 已赋值）与 ranking stage。
func applyRerank(
	ctx context.Context,
	client *ResolvedRerankClient,
	rankables []*rankableSearchResult,
	candidateTopK, maxDocumentChars int,
) ([]*rankableSearchResult, value.RankingStage, error) {
	if len(rankables) > candidateTopK {
		rankables = rankables[:candidateTopK]
	}
	if len(rankables) == 0 {
		return rankables, value.RankingStageRerank, nil
	}
	documents, err := buildRerankDocuments(rankables, maxDocumentChars)
	if err != nil {
		return nil, "", err
	}
	rerankResult, err := client.Client.Rerank(ctx, rerankport.RerankInput{
		Documents: documents,
		TopN:      len(documents),
	})
	if err != nil {
		return nil, "", err
	}
	scoreByID := make(map[string]float64, len(rerankResult.Items))
	for _, item := range rerankResult.Items {
		scoreByID[item.DocumentID] = item.Score
	}
	for index, item := range rankables {
		score := scoreByID[opaqueCandidateID(index)]
		item.RerankScore = &score
	}
	// 稳定排序：rerank DESC, RRF score DESC，原顺序作为最终 tie-break（已是 RRF 稳定序）。
	stableRerankSort(rankables)
	// 把 rerank score 回写到 SearchResult，并按新顺序返回 results。
	ordered := make([]*rankableSearchResult, len(rankables))
	for i, item := range rankables {
		if item.RerankScore != nil {
			score := *item.RerankScore
			item.Result.RerankScore = &score
		}
		ordered[i] = item
	}
	return ordered, value.RankingStageRerank, nil
}

// buildRerankDocuments 按文档类型构造私有重排文本：
// - FAQ：只使用命中 entry 的 search_content（问题集合），不使用返回用回答。
// - file/web：拼接去重后的 matched child search_content。
// - flat：使用 search_content。
// 超出 maxDocumentChars 时按 rune 截断。
func buildRerankDocuments(rankables []*rankableSearchResult, maxDocumentChars int) ([]rerankport.Document, error) {
	documents := make([]rerankport.Document, 0, len(rankables))
	for index, item := range rankables {
		text, err := buildRerankText(item, maxDocumentChars)
		if err != nil {
			return nil, err
		}
		documents = append(documents, rerankport.Document{
			ID:   opaqueCandidateID(index),
			Text: text,
		})
	}
	return documents, nil
}

func buildRerankText(item *rankableSearchResult, maxDocumentChars int) (string, error) {
	matched := dedupePreservingOrder(item.MatchedSearchContent)
	if len(matched) > 0 {
		joined := strings.Join(matched, "\n")
		return truncateRunes(joined, maxDocumentChars), nil
	}
	return "", fmt.Errorf("%w: 重排候选缺少文本", domainerrors.ErrInvalidRerankResponse)
}

func dedupePreservingOrder(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		if _, duplicate := seen[item]; duplicate {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

// opaqueCandidateID 生成稳定的短生命周期 opaque 候选 ID。
func opaqueCandidateID(index int) string {
	return fmt.Sprintf("candidate-%06d", index+1)
}

// stableRerankSort 对 rankable 做稳定排序：rerank DESC, RRF score DESC。
func stableRerankSort(rankables []*rankableSearchResult) {
	for i := 1; i < len(rankables); i++ {
		for j := i; j > 0 && rerankLess(rankables[j], rankables[j-1]); j-- {
			rankables[j], rankables[j-1] = rankables[j-1], rankables[j]
		}
	}
}

func rerankLess(a, b *rankableSearchResult) bool {
	ar, br := rerankScoreOf(a), rerankScoreOf(b)
	if ar != br {
		return ar > br
	}
	if a.Result != nil && b.Result != nil && a.Result.Score != b.Result.Score {
		return a.Result.Score > b.Result.Score
	}
	return false
}

func rerankScoreOf(item *rankableSearchResult) float64 {
	if item == nil || item.RerankScore == nil {
		return 0
	}
	return *item.RerankScore
}

// rerankSnapshotKey 计算用于多库一致性比对的快照键。
type rerankSnapshotKey struct {
	Enabled         bool
	ModelID         uuid.UUID
	ProviderID      uuid.UUID
	ModelName       string
	ModelConfigHash string
	CandidateTopK   int
	FailureMode     value.RerankFailureMode
}

func rerankKeyFromSnapshot(snapshot *model.RerankSnapshot) rerankSnapshotKey {
	if snapshot == nil {
		return rerankSnapshotKey{Enabled: false}
	}
	return rerankSnapshotKey{
		Enabled: true, ModelID: snapshot.ModelID, ProviderID: snapshot.ProviderID,
		ModelName: snapshot.ModelName, ModelConfigHash: snapshot.ModelConfigHash,
		CandidateTopK: snapshot.CandidateTopK, FailureMode: snapshot.FailureMode,
	}
}

// isRerankRecoverable 判断错误是否属于可回退的远端暂时故障。
func isRerankRecoverable(err error) bool {
	return errorIsAny(err,
		domainerrors.ErrRerankUnavailable,
		domainerrors.ErrRerankRateLimited,
		domainerrors.ErrRequestTimeout,
		domainerrors.ErrEndpointUnreachable,
		domainerrors.ErrProviderRejected,
		domainerrors.ErrInvalidRerankResponse,
	)
}

func errorIsAny(err error, targets ...error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
