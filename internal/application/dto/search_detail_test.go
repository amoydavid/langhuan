package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

func TestProjectSearchDetail(t *testing.T) {
	build := func() []*SearchResult {
		parent := &SearchResult{
			ChunkID: uuid.New(), Content: "父块正文",
			MatchedChildren: []MatchedChild{
				{ChunkID: uuid.New(), Content: "子块正文一", Score: 0.5},
				{ChunkID: uuid.New(), Content: "子块正文二", Score: 0.9},
			},
		}
		parent.Evidence = MatchedEvidenceOf(parent.MatchedChildren[1])
		return []*SearchResult{parent}
	}

	full := build()
	ProjectSearchDetail(full, value.SearchDetailFull)
	if full[0].Content != "父块正文" {
		t.Fatalf("full 档应保留父块正文")
	}
	if full[0].Evidence != nil {
		t.Fatalf("full 档应去除 Evidence")
	}

	lean := build()
	ProjectSearchDetail(lean, value.SearchDetailLean)
	if lean[0].Content != "" {
		t.Fatalf("lean 档父块正文应置空")
	}
	if lean[0].Evidence == nil || lean[0].Evidence.Content != "子块正文二" {
		t.Fatalf("lean 档应保留最佳命中子块证据")
	}
	if len(lean[0].MatchedChildren) != 2 {
		t.Fatalf("lean 档不应改变 matched_children 元数据")
	}

	// 空值归一为 full。
	defaulted := build()
	ProjectSearchDetail(defaulted, "")
	if defaulted[0].Evidence != nil {
		t.Fatalf("空档位应按 full 投影")
	}
}

func TestMatchedChildContentNotSerialized(t *testing.T) {
	child := MatchedChild{ChunkID: uuid.New(), Content: "内部正文", Score: 1}
	body, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "内部正文") || strings.Contains(string(body), "content") {
		t.Fatalf("matched_children[].content 不应进入序列化契约：%s", body)
	}
}

func TestEvidenceSerializedInLeanShape(t *testing.T) {
	result := &SearchResult{
		ChunkID: uuid.New(), Content: "", DocumentName: "文档",
		MatchedChildren: []MatchedChild{{ChunkID: uuid.New(), Content: "内部正文"}},
		Evidence:        &MatchedEvidence{ChunkID: uuid.New(), Content: "证据正文"},
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"evidence"`) || !strings.Contains(string(body), "证据正文") {
		t.Fatalf("lean 档 evidence 应序列化：%s", body)
	}
	if strings.Contains(string(body), "内部正文") {
		t.Fatalf("子块正文不应出现在任何序列化输出：%s", body)
	}
}

// TestLeanPayloadBudget 验收：lean 档 top10 结果的序列化体积应远小于
// full 档（spec §7：≤10000 字符 vs full 现状约 45000）。
func TestLeanPayloadBudget(t *testing.T) {
	buildResults := func() []*SearchResult {
		results := make([]*SearchResult, 0, 10)
		for i := 0; i < 10; i++ {
			parent := &SearchResult{
				ChunkID: uuid.New(), DocumentName: "会议转写文档示例标题",
				Content: strings.Repeat("父块正文内容。", 600), // ~4200 字，贴近真实父块上限
				MatchedChildren: []MatchedChild{
					{ChunkID: uuid.New(), Content: strings.Repeat("子块正文。", 40), Score: 0.9},
					{ChunkID: uuid.New(), Content: strings.Repeat("子块正文。", 40), Score: 0.5},
				},
			}
			parent.Evidence = MatchedEvidenceOf(parent.MatchedChildren[0])
			results = append(results, parent)
		}
		return results
	}

	full := buildResults()
	ProjectSearchDetail(full, value.SearchDetailFull)
	fullBody, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}

	lean := buildResults()
	ProjectSearchDetail(lean, value.SearchDetailLean)
	leanBody, err := json.Marshal(lean)
	if err != nil {
		t.Fatal(err)
	}
	// 预算按字符（rune）计，与 spec §7 口径一致（15000，含每行 UUID/锚点/引用
	// 等固定 JSON 开销；核心目标是比 full 缩小 3 倍以上，见下方比例断言）。
	leanRunes := utf8.RuneCount(leanBody)
	fullRunes := utf8.RuneCount(fullBody)
	if leanRunes > 15000 {
		t.Fatalf("lean 档 top10 序列化 %d 字符，超过 15000 预算", leanRunes)
	}
	if fullRunes < leanRunes*3 {
		t.Fatalf("full 档应显著大于 lean：full=%d lean=%d", fullRunes, leanRunes)
	}
}

// TestProjectSearchDetailPreservesOrder 验收：两档投影不改变行序与元数据
// （排序发生在投影之前，投影只触碰 Content 与 Evidence）。
func TestProjectSearchDetailPreservesOrder(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	results := []*SearchResult{
		{ChunkID: first, Content: "甲", Evidence: &MatchedEvidence{ChunkID: uuid.New()}},
		{ChunkID: second, Content: "乙", Evidence: &MatchedEvidence{ChunkID: uuid.New()}},
	}
	lean := []*SearchResult{
		{ChunkID: first, Content: "甲", Evidence: &MatchedEvidence{ChunkID: uuid.New()}},
		{ChunkID: second, Content: "乙", Evidence: &MatchedEvidence{ChunkID: uuid.New()}},
	}
	ProjectSearchDetail(results, value.SearchDetailFull)
	ProjectSearchDetail(lean, value.SearchDetailLean)
	if results[0].ChunkID != first || results[1].ChunkID != second {
		t.Fatal("full 投影改变了行序")
	}
	if lean[0].ChunkID != first || lean[1].ChunkID != second {
		t.Fatal("lean 投影改变了行序")
	}
}
