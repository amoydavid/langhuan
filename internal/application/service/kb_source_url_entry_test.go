package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 document_ingest url 入口（v1.3.0 spec §9.3）的应用层用例：
// AddURLToWebSource 要求目标 KB 恰有一个 mode=urls 且 active 的 web 源——
// 没有则提示先创建（含仅有非 urls 源/源被停用两种形态），多个则列出候选；
// URL 规范化后并入现有 urls（重复 URL 不重复追加但仍触发同步），复用
// UpdateWebSource 完成全量重校验与入队。判断依据与 KB 自身 source_type 无关。

// seedWebSourceWith 向 fake store 预置一个指定模式与状态的 web 源。
func (f *kbSourceFixture) seedWebSourceWith(t *testing.T, name, mode string, status value.KbSourceStatus) *model.KbSource {
	t.Helper()
	config := map[string]any{"mode": mode}
	switch mode {
	case string(value.WebSourceModeURLs):
		config["urls"] = []any{"https://example.com/a"}
	case string(value.WebSourceModeCrawl):
		config["crawl"] = map[string]any{"seed_url": "https://example.com/"}
	case string(value.WebSourceModeSitemap):
		config["sitemap"] = map[string]any{"url": "https://example.com/sitemap.xml"}
	}
	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: f.kb.WorkspaceID, KnowledgeBaseID: f.kb.ID,
		Kind: value.KbSourceKindWeb, Name: name, Status: status, Config: config,
	})
	require.NoError(t, err)
	f.store.sources[source.ID] = source
	return source
}

// storedURLsOf 解析 store 中指定源的 urls 列表（typed 解析，非法配置直接 Fatal）。
func storedURLsOf(t *testing.T, source *model.KbSource) []string {
	t.Helper()
	typed, err := value.ParseWebSourceConfig(source.Config, value.WebSourceLimits{MaxPagesCeiling: 100, MaxDepthCeiling: 5})
	require.NoError(t, err)
	return typed.URLs
}

func TestAddURLToWebSourceAppendsAndEnqueues(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "帮助中心")

	updated, job, err := fixture.svc.AddURLToWebSource(context.Background(),
		fixture.kb.WorkspaceID, fixture.kb.ID, " https://Example.com/b/?utm_source=x#frag ")
	require.NoError(t, err)
	require.Same(t, fixture.store.sources[source.ID], updated)
	require.NotNil(t, job)
	require.Equal(t, []uuid.UUID{source.ID}, fixture.enqueuer.calls)

	urls := storedURLsOf(t, fixture.store.sources[source.ID])
	// 规范化：host 小写、去跟踪参数与 fragment（原 path 非空则保留）。
	require.Equal(t, []string{"https://example.com/a", "https://example.com/b/"}, urls)
}

func TestAddURLToWebSourceDuplicateStillSyncs(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "帮助中心")

	_, job, err := fixture.svc.AddURLToWebSource(context.Background(),
		fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/a#anchor")
	require.NoError(t, err)
	// 已存在（规范化相等）不重复追加，但仍触发一次同步。
	require.Len(t, storedURLsOf(t, fixture.store.sources[source.ID]), 1)
	require.NotNil(t, job)
	require.Equal(t, []uuid.UUID{source.ID}, fixture.enqueuer.calls)
}

func TestAddURLToWebSourceRejectsInvalidURL(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "帮助中心")

	_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
		fixture.kb.WorkspaceID, fixture.kb.ID, "ftp://example.com/page")
	require.ErrorIs(t, err, domainerrors.ErrValidation)
	// 失败不落库、不入队。
	require.Equal(t, []string{"https://example.com/a"}, storedURLsOf(t, fixture.store.sources[source.ID]))
	require.Empty(t, fixture.enqueuer.calls)
}

func TestAddURLToWebSourceSelectionMatrix(t *testing.T) {
	t.Run("没有任何 web 源提示先创建", func(t *testing.T) {
		fixture := newKbSourceFixture(10)
		_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
			fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		require.Contains(t, err.Error(), "还没有网站内容源")
		require.Empty(t, fixture.enqueuer.calls)
	})

	t.Run("只有非 urls 模式源", func(t *testing.T) {
		fixture := newKbSourceFixture(10)
		crawl := fixture.seedWebSourceWith(t, "产品文档站", "crawl", value.KbSourceStatusActive)
		_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
			fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		require.Contains(t, err.Error(), "mode=urls")
		// 错误列出现有源，帮助调用方理解现状。
		require.Contains(t, err.Error(), crawl.Name)
		require.Contains(t, err.Error(), "crawl")
		require.Empty(t, fixture.enqueuer.calls)
	})

	t.Run("urls 源被停用视同没有", func(t *testing.T) {
		fixture := newKbSourceFixture(10)
		disabled := fixture.seedWebSourceWith(t, "帮助中心", "urls", value.KbSourceStatusDisabled)
		_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
			fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		require.Contains(t, err.Error(), "mode=urls")
		require.Contains(t, err.Error(), disabled.Name)
		require.Contains(t, err.Error(), string(value.KbSourceStatusDisabled))
		require.Empty(t, fixture.enqueuer.calls)
	})

	t.Run("多个 urls active 源列出候选", func(t *testing.T) {
		fixture := newKbSourceFixture(10)
		first := fixture.seedWebSourceWith(t, "源甲", "urls", value.KbSourceStatusActive)
		second := fixture.seedWebSourceWith(t, "源乙", "urls", value.KbSourceStatusActive)
		_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
			fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		for _, candidate := range []*model.KbSource{first, second} {
			require.Contains(t, err.Error(), candidate.Name)
			require.Contains(t, err.Error(), candidate.ID.String())
		}
		require.Empty(t, fixture.enqueuer.calls)
	})
}

func TestAddURLToWebSourceValidation(t *testing.T) {
	fixture := newKbSourceFixture(10)

	t.Run("lineage 缺失", func(t *testing.T) {
		_, _, err := fixture.svc.AddURLToWebSource(context.Background(), uuid.Nil, fixture.kb.ID, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		_, _, err = fixture.svc.AddURLToWebSource(context.Background(), fixture.kb.WorkspaceID, uuid.Nil, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
	})

	t.Run("url 为空白", func(t *testing.T) {
		_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
			fixture.kb.WorkspaceID, fixture.kb.ID, "   ")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		require.Contains(t, err.Error(), "url 不能为空")
	})

	t.Run("跨 workspace 的源不可见", func(t *testing.T) {
		other := newKbSourceFixture(10)
		source := other.seedWebSource(t, "别的库的源")
		// 用别的 workspace 的 lineage 调用：看不见 fixture 的源（若 fixture 也有源，
		// 该调用也不得命中 other 的源行）。
		_, _, err := other.svc.AddURLToWebSource(context.Background(),
			fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
		require.ErrorIs(t, err, domainerrors.ErrValidation)
		require.Contains(t, err.Error(), "还没有网站内容源")
		require.Empty(t, other.enqueuer.calls)
		require.Len(t, storedURLsOf(t, other.store.sources[source.ID]), 1)
	})
}

// TestAddURLToWebSourceOnlyCountsWebSources 锁定「upload KB 同样可以有 web 源」：
// 源选择只看 kb_sources 行本身（kind/status/mode），不读 KB 的 source_type。
func TestAddURLToWebSourceOnlyCountsWebSources(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.seedWebSource(t, "帮助中心")
	// KB 行不带任何 web 语义标记，也不影响选择。
	require.Empty(t, fixture.kb.SourceType)
	_, job, err := fixture.svc.AddURLToWebSource(context.Background(),
		fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
	require.NoError(t, err)
	require.NotNil(t, job)
}

// TestAddURLToWebSourceDeterministicCandidateOrder 多候选错误信息按名称排序
// （字节序，确定性，不依赖 store 的 map 遍历顺序）。
func TestAddURLToWebSourceDeterministicCandidateOrder(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.seedWebSourceWith(t, "zeta 源", "urls", value.KbSourceStatusActive)
	fixture.seedWebSourceWith(t, "alpha 源", "urls", value.KbSourceStatusActive)
	_, _, err := fixture.svc.AddURLToWebSource(context.Background(),
		fixture.kb.WorkspaceID, fixture.kb.ID, "https://example.com/b")
	require.Error(t, err)
	require.Less(t, strings.Index(err.Error(), "alpha 源"), strings.Index(err.Error(), "zeta 源"))
}
