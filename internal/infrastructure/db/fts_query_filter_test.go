package db

import (
	"reflect"
	"testing"
)

func TestFilterFTSQueryTokensQuestionQuery(t *testing.T) {
	// gse 对 "埃及有哪些民族？" 的实际切分（实测）：[埃及 有 哪些 民族 ？]
	got := FilterFTSQueryTokens([]string{"埃及", "有", "哪些", "民族", "？"})
	want := []string{"埃及", "民族"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter = %v, want %v", got, want)
	}
}

func TestFilterFTSQueryTokensKeepsKeywordQueries(t *testing.T) {
	// 关键词型 query 不应被过滤伤害：编号、术语、专有名词原样保留。
	got := FilterFTSQueryTokens([]string{"巴拿马运河", "全长", "多少", "？"})
	want := []string{"巴拿马运河", "全长"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter = %v, want %v", got, want)
	}
	got = FilterFTSQueryTokens([]string{"ISO-27001", "审计", "checklist", "v2"})
	want = []string{"ISO-27001", "审计", "checklist", "v2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keyword query 被误过滤: %v", got)
	}
}

func TestFilterFTSQueryTokensAllStopwordsYieldsNil(t *testing.T) {
	if got := FilterFTSQueryTokens([]string{"有", "哪些", "？", "什么"}); got != nil {
		t.Fatalf("全部停用词应返回 nil，got %v", got)
	}
}

func TestFilterFTSQueryTokensEnglishLowercasedStopwords(t *testing.T) {
	got := FilterFTSQueryTokens([]string{"What", "is", "refund", "policy"})
	want := []string{"refund", "policy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter = %v, want %v", got, want)
	}
}

func TestFilterFTSQueryTokensSingleCharContentWordKept(t *testing.T) {
	// 单字实义词（如"光"、"水"、"税"）不在虚词表内，必须保留。
	got := FilterFTSQueryTokens([]string{"光", "的", "折射"})
	want := []string{"光", "折射"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("单字实义词被误杀: %v", got)
	}
}
