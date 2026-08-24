package main

import (
	"reflect"
	"testing"
)

func TestArticleOfAndPassageSequence(t *testing.T) {
	if got := articleOf("7#12"); got != "7" {
		t.Fatalf("articleOf = %q, want 7", got)
	}
	if got := articleOf("no-separator"); got != "no-separator" {
		t.Fatalf("articleOf = %q", got)
	}
	if got := passageSequence("7#12"); got != 12 {
		t.Fatalf("passageSequence = %d, want 12", got)
	}
	if got := passageSequence("7#x"); got != 0 {
		t.Fatalf("passageSequence fallback = %d, want 0", got)
	}
}

func TestBuildTrackADeterministic(t *testing.T) {
	gold := map[string]struct{}{"100#1": {}}
	goldPassages := map[string]miraclPassage{
		"100#1": {DocID: "100#1", Title: "金", Text: "gold 文本"},
	}
	pool := []miraclPassage{
		{DocID: "9#0", Title: "a", Text: "干扰一"},
		{DocID: "5#3", Title: "b", Text: "干扰二"},
		{DocID: "100#1", Title: "金", Text: "gold 文本"}, // 干扰池中的 gold 必须去重
	}
	first := buildTrackA(goldPassages, gold, pool, 2)
	second := buildTrackA(goldPassages, gold, pool, 2)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("buildTrackA 不确定：%v vs %v", first, second)
	}
	if len(first) != 3 {
		t.Fatalf("track A size = %d, want 3（gold 1 + 干扰 2）", len(first))
	}
	sawGold := false
	for _, doc := range first {
		if doc.DocID == "100#1" {
			sawGold = true
		}
	}
	if !sawGold {
		t.Fatal("gold 段落缺失")
	}
}

func TestBuildTrackBDeterministicAndCapped(t *testing.T) {
	goldArticles := map[string]struct{}{"42": {}}
	articlePassages := map[string][]miraclPassage{
		"42": {
			{DocID: "42#9", Title: "文章", Text: "尾段"},
			{DocID: "42#1", Title: "文章", Text: "首段"},
		},
	}
	pool := map[string][]miraclPassage{
		"7": {
			{DocID: "7#0", Title: "A", Text: "a0"},
			{DocID: "7#1", Title: "A", Text: "a1"},
			{DocID: "7#2", Title: "A", Text: "a2"},
		},
		"8": {{DocID: "8#0", Title: "B", Text: "only-one"}}, // 段落不足 2，排除
	}
	first, _ := buildTrackB(articlePassages, goldArticles, pool, 1, 2)
	second, _ := buildTrackB(articlePassages, goldArticles, pool, 1, 2)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("buildTrackB 不确定：%v vs %v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("track B size = %d, want 2（gold 1 + 干扰 1）", len(first))
	}
	for _, doc := range first {
		if doc.DocID == "42" {
			if len(doc.Passages) != 2 || doc.Passages[0] != "首段" {
				t.Fatalf("gold 文章段落未按段落号排序/截断：%v", doc)
			}
		}
	}
}

func TestDeterministicVector(t *testing.T) {
	first := deterministicVector("同一文本", 8)
	second := deterministicVector("同一文本", 8)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("同文本向量不确定")
	}
	other := deterministicVector("不同文本", 8)
	if reflect.DeepEqual(first, other) {
		t.Fatal("不同文本不应产生相同向量")
	}
}
