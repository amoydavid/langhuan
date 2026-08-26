package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestVCSumMeetingAligned(t *testing.T) {
	meeting := vcsumMeeting{
		ID:       "1",
		EOSIndex: []int{1, 3},
		Context:  [][]string{{"a1", "a2"}, {"b1"}, {"c1", "c2"}, {"d1"}},
	}
	segments := map[string]map[int]vcsumSegment{
		"1": {
			0: {ID: "1_0", Context: [][]string{{"a1", "a2"}, {"b1"}}},
			1: {ID: "1_1", Context: [][]string{{"c1", "c2"}, {"d1"}}},
		},
	}
	if !vcsumMeetingAligned(meeting, segments) {
		t.Fatalf("完全一致的切分应当对齐")
	}

	cases := []struct {
		name    string
		mutate  func(m vcsumMeeting, s map[string]map[int]vcsumSegment) (vcsumMeeting, map[string]map[int]vcsumSegment)
		aligned bool
	}{
		{"末段下标不覆盖全文", func(m vcsumMeeting, s map[string]map[int]vcsumSegment) (vcsumMeeting, map[string]map[int]vcsumSegment) {
			m.EOSIndex = []int{1, 2}
			return m, s
		}, false},
		{"段记录缺失", func(m vcsumMeeting, s map[string]map[int]vcsumSegment) (vcsumMeeting, map[string]map[int]vcsumSegment) {
			delete(s["1"], 1)
			return m, s
		}, false},
		{"段 utterance 不一致", func(m vcsumMeeting, s map[string]map[int]vcsumSegment) (vcsumMeeting, map[string]map[int]vcsumSegment) {
			segment := s["1"][1]
			segment.Context = [][]string{{"c1"}}
			s["1"][1] = segment
			return m, s
		}, false},
		{"句子内容不同", func(m vcsumMeeting, s map[string]map[int]vcsumSegment) (vcsumMeeting, map[string]map[int]vcsumSegment) {
			segment := s["1"][0]
			segment.Context = [][]string{{"a1", "a3"}, {"b1"}}
			s["1"][0] = segment
			return m, s
		}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			m, s := testCase.mutate(meeting, map[string]map[int]vcsumSegment{
				"1": {
					0: {ID: "1_0", Context: [][]string{{"a1", "a2"}, {"b1"}}},
					1: {ID: "1_1", Context: [][]string{{"c1", "c2"}, {"d1"}}},
				},
			})
			if got := vcsumMeetingAligned(m, s); got != testCase.aligned {
				t.Fatalf("vcsumMeetingAligned = %v, want %v", got, testCase.aligned)
			}
		})
	}
}

func TestVCSumSegmentStartAndDocIDs(t *testing.T) {
	meeting := vcsumMeeting{ID: "7", EOSIndex: []int{2, 5, 6}}
	if got := vcsumSegmentStart(meeting, 0); got != 0 {
		t.Fatalf("seg0 start = %d, want 0", got)
	}
	if got := vcsumSegmentStart(meeting, 1); got != 3 {
		t.Fatalf("seg1 start = %d, want 3", got)
	}
	if got := vcsumSegmentStart(meeting, 2); got != 6 {
		t.Fatalf("seg2 start = %d, want 6", got)
	}
	if got := vcsumMeetingDocID("7"); got != "vcsum-m7" {
		t.Fatalf("meeting docid = %q", got)
	}
	segmentID := vcsumSegmentDocID("7", 2)
	if segmentID != "vcsum-m7#2" {
		t.Fatalf("segment docid = %q", segmentID)
	}
	// track-b 归因依赖 '#' 前缀解析出会议 docid。
	if got := articleOf(segmentID); got != "vcsum-m7" {
		t.Fatalf("articleOf(%q) = %q, want vcsum-m7", segmentID, got)
	}
}

func TestVCSumUtterancesJoinsSentences(t *testing.T) {
	got := vcsumUtterances([][]string{{"第一句。", "第二句。"}, {"第三句。"}})
	want := []string{"第一句。\n第二句。", "第三句。"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("utterance[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestVCSumTrackBPassagesHeadingVariants(t *testing.T) {
	meeting := vcsumMeeting{
		ID:       "1",
		EOSIndex: []int{1, 2},
		Context:  [][]string{{"a1"}, {"b1"}, {"c1"}},
	}
	utterances := vcsumUtterances(meeting.Context)

	plain := vcsumTrackBPassages(vcsumVariantPlain, meeting, utterances, nil)
	if len(plain) != 3 {
		t.Fatalf("plain passages = %v, want 原样 3 段", plain)
	}

	neutral := vcsumTrackBPassages(vcsumVariantHeadingNeutral, meeting, utterances, map[int]string{0: "话题段0", 1: "话题段1"})
	want := []string{"## 话题段0", "a1", "b1", "## 话题段1", "c1"}
	if len(neutral) != len(want) {
		t.Fatalf("neutral len = %d, want %d", len(neutral), len(want))
	}
	for index := range want {
		if neutral[index] != want[index] {
			t.Fatalf("neutral[%d] = %q, want %q", index, neutral[index], want[index])
		}
	}

	heading := vcsumTrackBPassages(vcsumVariantHeading, meeting, utterances, map[int]string{0: "议题甲", 1: "议题乙"})
	if heading[0] != "## 议题甲" || heading[3] != "## 议题乙" {
		t.Fatalf("heading passages = %v", heading)
	}
	// 标题缺失的段回退中性标题，保证注入不缺段。
	fallback := vcsumTrackBPassages(vcsumVariantHeadingLLM, meeting, utterances, map[int]string{})
	if fallback[0] != "## 话题段0" || fallback[3] != "## 话题段1" {
		t.Fatalf("llm 回退 passages = %v", fallback)
	}
}

func TestCleanVCSumLLMTitle(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"DeFi变革与展望", "DeFi变革与展望"},
		{"  \"电影产业与资本的对话\"。", "电影产业与资本的对话"},
		{"国际货币体系改革\n\n（补充说明）", "国际货币体系改革"},
		{"汽车芯片短缺与应对策略!", "汽车芯片短缺与应对策略"},
		{"「元宇宙的生态发展需完善」", "元宇宙的生态发展需完善"},
		{"  ", ""},
	}
	for _, testCase := range cases {
		if got := cleanVCSumLLMTitle(testCase.raw); got != testCase.want {
			t.Fatalf("cleanVCSumLLMTitle(%q) = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
	long := strings.Repeat("题", 40)
	if got := cleanVCSumLLMTitle(long); utf8.RuneCountInString(got) != 30 {
		t.Fatalf("超长标题未截断到 30 字：%d", utf8.RuneCountInString(got))
	}
}

func TestVCSumDatasetDirName(t *testing.T) {
	if got := vcsumDatasetDirName(vcsumVariantPlain); got != "vcsum" {
		t.Fatalf("plain dir = %q", got)
	}
	if got := vcsumDatasetDirName(vcsumVariantHeading); got != "vcsum-heading" {
		t.Fatalf("heading dir = %q", got)
	}
	if got := vcsumDatasetDirName(vcsumVariantHeadingNeutral); got != "vcsum-heading-neutral" {
		t.Fatalf("neutral dir = %q", got)
	}
}

func TestVCSumQueryAssetCoversOnlyQueryMeetings(t *testing.T) {
	var asset []vcsumQueryAssetItem
	if err := json.Unmarshal(vcsumQueryAsset, &asset); err != nil {
		t.Fatalf("解析 query 资产失败: %v", err)
	}
	if len(asset) != 139 {
		t.Fatalf("query 数 = %d, want 139", len(asset))
	}
	seen := map[string]struct{}{}
	for _, item := range asset {
		key := fmt.Sprintf("%s#%d", item.Meeting, item.Seg)
		if _, ok := seen[key]; ok {
			t.Fatalf("query 资产重复引用段 %s", key)
		}
		seen[key] = struct{}{}
		if item.Query == "" {
			t.Fatalf("段 %s 的 query 为空", key)
		}
	}
}
