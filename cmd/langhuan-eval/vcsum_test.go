package main

import (
	"encoding/json"
	"fmt"
	"testing"
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
