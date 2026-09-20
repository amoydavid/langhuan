package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/value"
)

// extractFixture 构造一个接近真实站点的假页面：高链接密度的导航/侧栏/页脚包裹
// 纯散文正文，用于区分抽取模式的作用范围（readability 依赖链接密度等启发式）。
func extractFixture(article string) []byte {
	return []byte(`<!DOCTYPE html><html><head><title>测试标题</title></head><body>
<header><nav class="site-nav">` + navLinks("站点导航唯一标识词") + `</nav></header>
<main><article class="content"><p>` + article + `</p><p>` + article + `</p><p>` + article + `</p></article></main>
<aside class="sidebar">` + navLinks("侧边栏唯一标识词") + `</aside>
<footer>` + navLinks("页脚唯一标识词") + `</footer>
</body></html>`)
}

// navLinks 生成一段纯链接的噪声区块（只含 <a>，模拟导航/侧栏/页脚）。
func navLinks(label string) string {
	var builder strings.Builder
	for i := 0; i < 12; i++ {
		builder.WriteString(`<a href="/nav/`)
		builder.WriteString(strconv.Itoa(i))
		builder.WriteString(`">`)
		builder.WriteString(label)
		builder.WriteString(`</a>`)
	}
	return builder.String()
}

func TestExtractReadabilityModeProducesArticleMarkdown(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeReadability,
		FallbackToFull: true,
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 100},
	}

	result, err := Extract(extractFixture(longText(600)+" lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)
	require.NoError(t, err)

	assert.Contains(t, string(result.Markdown), "lanhuan-article-marker")
	assert.NotContains(t, string(result.Markdown), "站点导航唯一标识词")
	assert.Equal(t, "测试标题", result.Title)
	assert.Empty(t, result.Warnings)
}

func TestExtractFullModeKeepsWholePage(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:        value.WebSourceExtractModeFull,
		Readability: value.WebSourceReadabilityConfig{CharThreshold: 100},
	}

	result, err := Extract(extractFixture(longText(600)+" lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)

	markdown := string(result.Markdown)
	assert.Contains(t, markdown, "lanhuan-article-marker")
	assert.Contains(t, markdown, "站点导航唯一标识词", "full 模式不做 readability 抽取")
	assert.Equal(t, "测试标题", result.Title)
	assert.Empty(t, result.Warnings)
}

func TestExtractHonorsTargetSelector(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeFull,
		TargetSelector: ".content",
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 100},
	}

	result, err := Extract(extractFixture(longText(20)+" lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)

	markdown := string(result.Markdown)
	assert.Contains(t, markdown, "lanhuan-article-marker")
	assert.NotContains(t, markdown, "站点导航唯一标识词")
	assert.NotContains(t, markdown, "侧边栏唯一标识词")
	assert.Empty(t, result.Warnings)
}

func TestExtractWarnsAndFallsBackWhenTargetSelectorMisses(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeFull,
		TargetSelector: ".not-there",
	}

	result, err := Extract(extractFixture("lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)

	assert.Contains(t, string(result.Markdown), "lanhuan-article-marker")
	assert.Equal(t, []string{WarningTargetSelectorNoMatch}, result.Warnings)
}

func TestExtractWarnsReadabilityFallbackWhenArticleTooShort(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeReadability,
		FallbackToFull: true,
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 500},
	}

	result, err := Extract(extractFixture("短正文 lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)

	assert.Contains(t, string(result.Markdown), "lanhuan-article-marker")
	assert.Contains(t, result.Warnings, WarningReadabilityFallback)
}

func TestExtractWithoutFallbackKeepsReadabilityResult(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeReadability,
		FallbackToFull: false,
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 500},
	}

	result, err := Extract(extractFixture("短正文 lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)
	assert.NotContains(t, result.Warnings, WarningReadabilityFallback)
}

func TestExtractRewritesRelativeLinkAgainstPageURL(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	rawHTML := []byte(`<!DOCTYPE html><html><head><title>t</title></head><body>
<article><p>` + longText(600) + `</p><img src="/img/a.png" alt="a"></article>
</body></html>`)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeReadability,
		FallbackToFull: true,
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 100},
	}

	result, err := Extract(rawHTML, pageURL, cfg)
	require.NoError(t, err)
	assert.Contains(t, string(result.Markdown), "https://example.com/img/a.png",
		"相对图片链接必须绝对化，下游资产归档才能下载")
}

func TestExtractHandlesEmptyAndInvalidInput(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeReadability,
		FallbackToFull: true,
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 100},
	}

	empty, err := Extract(nil, pageURL, cfg)
	require.NoError(t, err)
	assert.Empty(t, empty.Markdown)
	assert.Empty(t, empty.Warnings)

	// pageURL 缺失（nil）时不得 panic：readability 与 html-to-markdown 都接受无 base。
	result, err := Extract(extractFixture(longText(600)+" lanhuan-article-marker"), nil, cfg)
	require.NoError(t, err)
	assert.Contains(t, string(result.Markdown), "lanhuan-article-marker")

	// 非法 CSS 选择器：不 panic，按未命中处理。
	cfg.TargetSelector = ">>><<<"
	cfg.Mode = value.WebSourceExtractModeFull
	result, err = Extract(extractFixture("lanhuan-article-marker"), pageURL, cfg)
	require.NoError(t, err)
	assert.Contains(t, string(result.Markdown), "lanhuan-article-marker")
	assert.Equal(t, []string{WarningTargetSelectorNoMatch}, result.Warnings)
}

func TestExtractReadabilityKeepsAllArticleSections(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://example.com/blog/post")
	require.NoError(t, err)

	// readability 实现把正文容器以 Node 暴露（而非 HTML 字符串）：本用例钉住
	// 「渲染 Node 不丢正文段落」这一语义，防止库升级后静默截断多段正文。
	rawHTML := []byte(`<!DOCTYPE html><html><head><title>测试标题</title></head><body>
<header><nav>` + navLinks("站点导航唯一标识词") + `</nav></header>
<main><article>
<p>` + longText(200) + ` 标记甲</p>
<p>` + longText(200) + ` 标记乙</p>
<p>` + longText(200) + ` 标记丙</p>
</article></main>
<footer>` + navLinks("页脚唯一标识词") + `</footer>
</body></html>`)

	cfg := value.WebSourceExtractConfig{
		Mode:           value.WebSourceExtractModeReadability,
		FallbackToFull: true,
		Readability:    value.WebSourceReadabilityConfig{CharThreshold: 100},
	}

	result, err := Extract(rawHTML, pageURL, cfg)
	require.NoError(t, err)
	assert.Empty(t, result.Warnings, "命中 readability 抽取时不应回落整页")

	markdown := string(result.Markdown)
	for _, marker := range []string{"标记甲", "标记乙", "标记丙"} {
		assert.Contains(t, markdown, marker)
	}
	assert.NotContains(t, markdown, "站点导航唯一标识词")
	assert.NotContains(t, markdown, "页脚唯一标识词")
}
