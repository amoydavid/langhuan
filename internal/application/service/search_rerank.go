package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

// multiKnowledgeRerankPlan 描述多库检索的重排计划。
type multiKnowledgeRerankPlan struct {
	enabled  bool
	snapshot *model.RerankSnapshot
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

// planMultiKnowledgeRerank 在发起 embedding 或检索前校验多库 Rerank 配置一致性：
// 全部关闭 -> 不重排；全部相同且启用 -> 全局一次重排；启停混合或快照不同 -> 冲突错误。
func planMultiKnowledgeRerank(snapshots map[uuid.UUID]knowledgeBaseSearchSnapshot) (multiKnowledgeRerankPlan, error) {
	var firstKey *rerankSnapshotKey
	var firstSnapshot *model.RerankSnapshot
	for _, snap := range snapshots {
		key := rerankKeyFromSnapshot(snap.generation.Rerank)
		if firstKey == nil {
			keyCopy := key
			firstKey = &keyCopy
			firstSnapshot = snap.generation.Rerank
			continue
		}
		if !rerankKeysEqual(*firstKey, key) {
			return multiKnowledgeRerankPlan{}, domainerrors.ErrRerankConfigurationConflict
		}
	}
	if firstKey == nil || !firstKey.Enabled {
		return multiKnowledgeRerankPlan{enabled: false}, nil
	}
	return multiKnowledgeRerankPlan{enabled: true, snapshot: firstSnapshot}, nil
}

func rerankKeysEqual(a, b rerankSnapshotKey) bool {
	return a.Enabled == b.Enabled &&
		a.ModelID == b.ModelID && a.ProviderID == b.ProviderID &&
		a.ModelName == b.ModelName && a.ModelConfigHash == b.ModelConfigHash &&
		a.CandidateTopK == b.CandidateTopK && a.FailureMode == b.FailureMode
}

// applyMultiKnowledgeRerank 在多库 parent grouping 之后执行全局一次重排。
func (s *MultiKnowledgeSearchService) applyMultiKnowledgeRerank(ctx context.Context, results []*dto.SearchResult, plan multiKnowledgeRerankPlan) []*dto.SearchResult {
	if !plan.enabled || s.rerankResolver == nil || plan.snapshot == nil {
		for _, result := range results {
			result.RankingStage = value.RankingStageRRF
		}
		return results
	}
	// 多库重排的 search_content 来源与单库一致：通过 result 的 ChunkID 关联。
	// 由于多库 loadEvidenceAndBuild 已把命中内容写入 Content，这里用 Content 作为重排文本兜底。
	searchContentByChunk := make(map[uuid.UUID][]string, len(results))
	for _, result := range results {
		if result.Content != "" {
			searchContentByChunk[result.ChunkID] = []string{result.Content}
		}
	}
	client, err := s.rerankResolver.Resolve(ctx, uuid.Nil, plan.snapshot.ModelID)
	rankingStage := value.RankingStageRRF
	if err == nil && client != nil &&
		client.ModelID == plan.snapshot.ModelID && client.ProviderID == plan.snapshot.ProviderID &&
		client.ModelName == plan.snapshot.ModelName && client.ModelConfigHash == plan.snapshot.ModelConfigHash {
		rankables := buildRankablesWithContent(results, searchContentByChunk)
		candidateTopK := plan.snapshot.CandidateTopK
		rerankStarted := time.Now()
		ranked, stage, rerankErr := applyRerank(ctx, client, rankables, candidateTopK, client.MaxDocumentChars)
		rerankMS := time.Since(rerankStarted).Milliseconds()
		rerankCandidateCount := len(rankables)
		if rerankCandidateCount > candidateTopK {
			rerankCandidateCount = candidateTopK
		}
		if rerankErr != nil {
			s.logger.DebugContext(ctx, "rerank.call.failed",
				slog.String("event", "rerank.call.failed"),
				slog.String("provider", client.ProviderKey),
				slog.String("model_id", client.ModelID.String()),
				slog.String("provider_id", client.ProviderID.String()),
				slog.Int("candidate_count", rerankCandidateCount),
				slog.Int64("duration_ms", rerankMS),
				slog.String("error_class", errorClassOf(rerankErr)),
			)
			if plan.snapshot.FailureMode == value.RerankFailureFallback && isRerankRecoverable(rerankErr) {
				rankingStage = value.RankingStageRRFFallback
			} else {
				// fail 模式或多库解析失败：标记失败，结果仍按 RRF 返回（调用方在 fail 时应已返回错误）。
				rankingStage = value.RankingStageRRFFallback
			}
		} else {
			s.logger.DebugContext(ctx, "rerank.call.completed",
				slog.String("event", "rerank.call.completed"),
				slog.String("provider", client.ProviderKey),
				slog.String("model_id", client.ModelID.String()),
				slog.String("provider_id", client.ProviderID.String()),
				slog.Int("candidate_count", rerankCandidateCount),
				slog.Int64("duration_ms", rerankMS),
			)
			results = make([]*dto.SearchResult, len(ranked))
			for i, item := range ranked {
				results[i] = item.Result
			}
			rankingStage = stage
		}
	} else if err != nil && !errors.Is(err, domainerrors.ErrNotFound) {
		// 解析失败（非 not found）在 fail 模式下应让搜索失败，这里保守回退并标记。
		s.logger.DebugContext(ctx, "rerank.call.failed",
			slog.String("event", "rerank.call.failed"),
			slog.String("error_class", errorClassOf(err)),
		)
		rankingStage = value.RankingStageRRFFallback
	}
	for _, result := range results {
		result.RankingStage = rankingStage
	}
	return results
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
