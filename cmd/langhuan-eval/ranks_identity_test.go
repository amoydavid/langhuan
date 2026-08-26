package main

import (
	"strings"
	"testing"
)

// TestRanksOfChildContentIdentity 证明 spec D6 的恒等式：命中判定从
// 「父块正文 + 命中子块正文拼接」改为「仅父块正文」后，命中排名逐位一致。
// 依据是构造性事实：子块正文 ⊆ 父块正文（chunker 父子装配即按序拼接），
// 拼接不增加任何 gold bigram 的覆盖。对照侧复刻 v1.1.x 的旧行为。
func TestRanksOfChildContentIdentity(t *testing.T) {
	parentA := "琅嬛是一个知识转化与检索服务，负责把文档转成可检索的结构，并提供溯源锚点与引用。"
	childA1 := "琅嬛是一个知识转化与检索服务"
	childA2 := "并提供溯源锚点与引用。"
	parentB := "检索评测使用字符 bigram 重叠率作为命中判定，阈值为零点六。"
	childB := "字符 bigram 重叠率"
	golds := []string{
		"知识转化与检索服务",
		"字符 bigram 重叠率作为命中判定",
		"完全不相关的第三段文字，用于验证不命中的分支",
	}
	items := []searchResultItem{
		{Content: parentA, MatchedChildren: []struct {
			ChunkID string `json:"chunk_id"`
			Content string `json:"content"`
		}{{ChunkID: "c1", Content: childA1}, {ChunkID: "c2", Content: childA2}}},
		{Content: parentB, MatchedChildren: []struct {
			ChunkID string `json:"chunk_id"`
			Content string `json:"content"`
		}{{ChunkID: "c3", Content: childB}}},
	}

	for _, threshold := range []float64{0.3, 0.6, 0.8} {
		newRanks := ranksOf(items, golds, threshold)
		oldRanks := legacyRanksWithChildren(items, golds, threshold)
		if len(newRanks) != len(oldRanks) {
			t.Fatalf("threshold=%v ranks 不一致：new=%v old=%v", threshold, newRanks, oldRanks)
		}
		for index := range newRanks {
			if newRanks[index] != oldRanks[index] {
				t.Fatalf("threshold=%v ranks[%d] 不一致：new=%d old=%d", threshold, index, newRanks[index], oldRanks[index])
			}
		}
	}
}

// legacyRanksWithChildren 复刻 v1.1.x 的命中判定：父块正文与命中子块正文拼接。
func legacyRanksWithChildren(items []searchResultItem, golds []string, threshold float64) []int {
	covered := make([]bool, len(golds))
	var ranks []int
	for position, item := range items {
		var builder strings.Builder
		builder.WriteString(item.Content)
		for _, child := range item.MatchedChildren {
			builder.WriteString("\n")
			builder.WriteString(child.Content)
		}
		content := builder.String()
		newlyCovered := false
		for index, gold := range golds {
			if !covered[index] && overlapRatio(content, gold) >= threshold {
				covered[index] = true
				newlyCovered = true
			}
		}
		if newlyCovered {
			ranks = append(ranks, position+1)
		}
	}
	return ranks
}
