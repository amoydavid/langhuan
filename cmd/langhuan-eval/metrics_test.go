package main

import (
	"math"
	"testing"
)

func TestCharBigramsChinese(t *testing.T) {
	grams := charBigrams("退款政策")
	if len(grams) != 3 {
		t.Fatalf("bigram count = %d, want 3", len(grams))
	}
	if _, ok := grams["退款"]; !ok {
		t.Fatalf("missing 退款: %v", grams)
	}
	if len(charBigrams("")) != 0 {
		t.Fatal("empty string should yield no bigrams")
	}
	if len(charBigrams("A")) != 1 {
		t.Fatal("single rune should fall back to unigram")
	}
}

func TestOverlapRatioContainment(t *testing.T) {
	gold := "如何申请退款"
	full := "客户询问如何申请退款的具体流程"
	if got := overlapRatio(full, gold); got != 1.0 {
		t.Fatalf("containment overlap = %v, want 1", got)
	}
	partial := overlapRatio("如何申请", gold)
	if partial <= 0 || partial >= 1 {
		t.Fatalf("partial overlap = %v, want (0,1)", partial)
	}
	if got := overlapRatio("完全不相关", gold); got != 0 {
		t.Fatalf("unrelated overlap = %v, want 0", got)
	}
	if got := overlapRatio("任意", ""); got != 0 {
		t.Fatalf("empty gold overlap = %v, want 0", got)
	}
}

func TestMatchesGoldThreshold(t *testing.T) {
	golds := []string{"如何申请退款"}
	if !matchesGold("如何申请退款与售后政策", golds, 0.6) {
		t.Fatal("full containment should match")
	}
	if matchesGold("无关内容", golds, 0.6) {
		t.Fatal("unrelated content should not match")
	}
}

func TestRecallMRRNDCG(t *testing.T) {
	// query1：gold 2 条，rank 1、4 命中；query2：gold 1 条，K 内未命中。
	evals := []queryEvaluation{{Ranks: []int{1, 4}}, {Ranks: []int{}}}
	golds := []int{2, 1}

	// query1 两条 gold 均在前 5 命中（覆盖 1.0），query2 未命中（0）：均值 0.5。
	if got := recallAtK(evals, golds, 5); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("recall@5 = %v, want 0.5", got)
	}
	// query1 rank1 命中 + query2 无命中：MRR = (1 + 0)/2。
	if got := mrrAtK(evals, 10); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("mrr@10 = %v, want 0.5", got)
	}
	// query1: DCG = 1/log2(2) + 1/log2(5)，IDCG = 1/log2(2) + 1/log2(3)；query2 = 0。
	dcg1 := 1/math.Log2(2) + 1/math.Log2(5)
	idcg1 := 1/math.Log2(2) + 1/math.Log2(3)
	want := (dcg1 / idcg1) / 2
	if got := ndcgAtK(evals, golds, 10); math.Abs(got-want) > 1e-9 {
		t.Fatalf("ndcg@10 = %v, want %v", got, want)
	}
}

func TestSummarizeDeterministic(t *testing.T) {
	evals := []queryEvaluation{{Ranks: []int{1}}, {Ranks: []int{3}}}
	golds := []int{1, 1}
	first := summarize(evals, golds)
	second := summarize(evals, golds)
	if first != second {
		t.Fatalf("summary not deterministic: %#v vs %#v", first, second)
	}
	if first.QueryCount != 2 {
		t.Fatalf("query count = %d", first.QueryCount)
	}
}
