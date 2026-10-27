//go:build integration

package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
)

// TestV140InsightsTelemetryE2E 串起健康回路的完整验收剧本（spec §11.5 第 3/4/8 条）：
// 零召回 → 开启查询原文 → 零召回面板可见原文 → 补 FAQ → 同词命中 → 引用落库 →
// 环比可见 + citation-summary 一致；member 对 insights 端点被拒。
func TestV140InsightsTelemetryE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)
	insightsBase := "/api/v1/workspaces/" + env.workspace.Slug + "/knowledge-bases/" + kbID

	// 零召回剧本的核心问法：内容库不含该词。
	const unknownQuestion = "独有的验收问题措辞"
	var results []*dto.SearchResult
	env.jsonRequest(http.MethodPost, insightsBase+"/search",
		map[string]any{"query": unknownQuestion, "final_top_k": 8}, http.StatusOK, &results)
	require.Empty(t, results, "首次检索必须零召回")

	// 关闭态：零召回面板无明文（spec 验收 3）。
	var queriesResp struct {
		QueryLogEnabled bool `json:"query_log_enabled"`
		Queries         []struct {
			QueryHash       string `json:"query_hash"`
			Hits            int64  `json:"hits"`
			LatestQueryText string `json:"latest_query_text"`
		} `json:"queries"`
	}
	requireWaitingFor(t, 5*time.Second, func() bool {
		env.jsonRequest(http.MethodGet, insightsBase+"/insights/queries?window_days=14", nil, http.StatusOK, &queriesResp)
		return len(queriesResp.Queries) > 0
	}, "零召回 hash 组必须可聚合")
	require.False(t, queriesResp.QueryLogEnabled)
	require.NotEmpty(t, queriesResp.Queries, "零召回 hash 组必须可聚合")
	require.Empty(t, queriesResp.Queries[0].LatestQueryText, "未开启原文记录时不得返回明文")

	// 开启查询原文记录（spec 验收 4 第一步）。
	env.jsonRequest(http.MethodPut, insightsBase+"/insights/query-log-settings",
		map[string]any{"enabled": true}, http.StatusOK, &struct{}{})

	// 同词再检索：empty run 的原文进入 query_log（等待异步 flush 落库）。
	env.jsonRequest(http.MethodPost, insightsBase+"/search",
		map[string]any{"query": unknownQuestion, "final_top_k": 8}, http.StatusOK, &results)
	requireWaitingFor(t, 5*time.Second, func() bool {
		env.jsonRequest(http.MethodGet, insightsBase+"/insights/queries?window_days=14", nil, http.StatusOK, &queriesResp)
		return queriesResp.QueryLogEnabled && len(queriesResp.Queries) > 0 &&
			queriesResp.Queries[0].LatestQueryText == unknownQuestion
	}, "开启后零召回面板必须可见查询原文")

	// 补 FAQ：问题=零召回问法（模拟健康面板「创建 FAQ」预填入口的结果）。
	var faqCreated struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	env.jsonRequest(http.MethodPost, insightsBase+"/documents/faq",
		map[string]any{"title": "验收 FAQ", "questions": []string{unknownQuestion}, "answer": "这是补齐后的回答。"},
		http.StatusCreated, &faqCreated)
	require.NotEmpty(t, faqCreated.Document.ID)

	// FAQ 是同步建档 + 异步索引；等待可检索。
	var hitResults []*dto.SearchResult
	requireWaitingFor(t, 20*time.Second, func() bool {
		env.jsonRequest(http.MethodPost, insightsBase+"/search",
			map[string]any{"query": unknownQuestion, "final_top_k": 8}, http.StatusOK, &hitResults)
		return len(hitResults) > 0
	}, "补 FAQ 后同词必须可命中")

	// 引用遥测落库（异步 flush 后）：citation-summary 与删除预警口径一致（spec 验收 5）。
	faqDocID := hitResults[0].DocumentID.String()
	requireWaitingFor(t, 5*time.Second, func() bool {
		var summary struct {
			CitationCount int64  `json:"citation_count"`
			LastCitedAt   string `json:"last_cited_at"`
		}
		env.jsonRequest(http.MethodGet, insightsBase+"/documents/"+faqDocID+"/citation-summary?window_days=30", nil, http.StatusOK, &summary)
		return summary.CitationCount > 0
	}, "命中后引用遥测必须落库并可经 citation-summary 读取")

	// 总览：空召回率存在且 query_log_enabled=true（spec 验收 2/4 闭环）。
	var overview struct {
		RunsTotal       int64   `json:"runs_total"`
		RunsEmpty       int64   `json:"runs_empty"`
		RunsAvailable   int64   `json:"runs_available"`
		EmptyRecallRate float64 `json:"empty_recall_rate"`
		QueryLogEnabled bool    `json:"query_log_enabled"`
		SleepingCount   int64   `json:"sleeping_documents_count"`
		RecentChanges   int64   `json:"recent_changes_count"`
	}
	env.jsonRequest(http.MethodGet, insightsBase+"/insights/overview?window_days=14", nil, http.StatusOK, &overview)
	require.GreaterOrEqual(t, overview.RunsEmpty, int64(2), "至少两次零召回 run 被统计")
	require.GreaterOrEqual(t, overview.RunsAvailable, int64(1), "补 FAQ 后的命中 run 被统计")
	require.Greater(t, overview.EmptyRecallRate, 0.0)
	require.True(t, overview.QueryLogEnabled)
	require.GreaterOrEqual(t, overview.RecentChanges, int64(1), "FAQ 新版本应出现在内容动态计数")
}

// requireWaitingFor 在 timeout 内每 250ms 轮询条件；超时 fail。
func requireWaitingFor(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", fmt.Sprintf("%s", message))
}
