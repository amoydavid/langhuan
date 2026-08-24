// Package main 实现琅嬛离线检索评测：指标计算（Recall/MRR/nDCG）与
// gold passage 文本重叠命中判定。指标定义与 qrels 二值相关性对齐 spec §8。
package main

import (
	"math"
	"sort"
)

// charBigrams 提取 Unicode 字符级 bigram 集合。中文没有天然词边界，
// 字符 bigram 是稳定的规范化单元；单字符文本回退为自身。
func charBigrams(text string) map[string]struct{} {
	runes := []rune(text)
	set := make(map[string]struct{}, len(runes))
	if len(runes) == 0 {
		return set
	}
	if len(runes) == 1 {
		set[string(runes)] = struct{}{}
		return set
	}
	for i := 0; i+1 < len(runes); i++ {
		set[string(runes[i:i+2])] = struct{}{}
	}
	return set
}

// overlapRatio 计算 candidate 相对 gold 的 bigram 包含率：
// |bigram(candidate) ∩ bigram(gold)| / |bigram(gold)|。
// gold 过短（无有效 bigram）时返回 0，避免退化匹配。
func overlapRatio(candidate, gold string) float64 {
	goldGrams := charBigrams(gold)
	if len(goldGrams) == 0 {
		return 0
	}
	candidateGrams := charBigrams(candidate)
	if len(candidateGrams) == 0 {
		return 0
	}
	intersect := 0
	for gram := range goldGrams {
		if _, ok := candidateGrams[gram]; ok {
			intersect++
		}
	}
	return float64(intersect) / float64(len(goldGrams))
}

// matchesGold 判定一条检索结果是否命中任一 gold passage（阈值制）。
func matchesGold(resultContent string, goldPassages []string, threshold float64) bool {
	for _, gold := range goldPassages {
		if overlapRatio(resultContent, gold) >= threshold {
			return true
		}
	}
	return false
}

// queryEvaluation 是单个 query 在一个通道组合下的命中排名列表
// （1-based；0 表示前 K 内未命中任何 gold）。
type queryEvaluation struct {
	Ranks []int
}

// Recall@K：前 K 结果覆盖的 gold 占比。多 gold 时按覆盖比例计入均值。
func recallAtK(evals []queryEvaluation, goldCounts []int, k int) float64 {
	if len(evals) == 0 {
		return 0
	}
	total := 0.0
	for index, evaluation := range evals {
		goldCount := goldCounts[index]
		if goldCount == 0 {
			continue
		}
		hit := map[int]struct{}{}
		for _, rank := range evaluation.Ranks {
			if rank <= k {
				hit[rank] = struct{}{}
			}
		}
		total += float64(len(hit)) / float64(goldCount)
	}
	return total / float64(len(evals))
}

// MRR@K：首个命中 gold 的倒数排名均值（K 内无命中计 0）。
func mrrAtK(evals []queryEvaluation, k int) float64 {
	if len(evals) == 0 {
		return 0
	}
	total := 0.0
	for _, evaluation := range evals {
		best := 0
		for _, rank := range evaluation.Ranks {
			if rank <= k && (best == 0 || rank < best) {
				best = rank
			}
		}
		if best > 0 {
			total += 1 / float64(best)
		}
	}
	return total / float64(len(evals))
}

// ndcgAtK：二值增益 nDCG。多命中按命中位置折损累计，IDCG 取前
// min(goldCount, K) 位全命中的理想值。
func ndcgAtK(evals []queryEvaluation, goldCounts []int, k int) float64 {
	if len(evals) == 0 {
		return 0
	}
	total := 0.0
	for index, evaluation := range evals {
		ranks := make([]int, len(evaluation.Ranks))
		copy(ranks, evaluation.Ranks)
		sort.Ints(ranks)
		dcg := 0.0
		for _, rank := range ranks {
			if rank >= 1 && rank <= k {
				dcg += 1 / math.Log2(float64(rank)+1)
			}
		}
		ideal := min(goldCounts[index], k)
		idcg := 0.0
		for position := 1; position <= ideal; position++ {
			idcg += 1 / math.Log2(float64(position)+1)
		}
		if idcg > 0 {
			total += dcg / idcg
		}
	}
	return total / float64(len(evals))
}

// metricsSummary 汇总一个轨道 × 通道组合的指标。
type metricsSummary struct {
	QueryCount int     `json:"query_count"`
	RecallAt5  float64 `json:"recall@5"`
	RecallAt10 float64 `json:"recall@10"`
	MRRAt10    float64 `json:"mrr@10"`
	NDCGAt10   float64 `json:"ndcg@10"`
}

func summarize(evals []queryEvaluation, goldCounts []int) metricsSummary {
	return metricsSummary{
		QueryCount: len(evals),
		RecallAt5:  round4(recallAtK(evals, goldCounts, 5)),
		RecallAt10: round4(recallAtK(evals, goldCounts, 10)),
		MRRAt10:    round4(mrrAtK(evals, 10)),
		NDCGAt10:   round4(ndcgAtK(evals, goldCounts, 10)),
	}
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}
