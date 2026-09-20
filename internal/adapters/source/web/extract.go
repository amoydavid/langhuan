package web

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/dajee/langhuan/internal/domain/value"
)

// 抽取层告警码（spec §7）。
const (
	// WarningReadabilityFallback 表示 readability 产物为空或低于 char_threshold，
	// 已按 fallback_to_full 回落为「整页（或 target_selector 圈定区）转 markdown」。
	WarningReadabilityFallback = "readability_fallback"
	// WarningTargetSelectorNoMatch 表示 target_selector 未命中任何节点（选择器写错、
	// 站点改版），已回退为整页处理：不报错，但必须可观测。
	WarningTargetSelectorNoMatch = "target_selector_no_match"
)

// ExtractResult 是一次正文抽取的结果。
type ExtractResult struct {
	// Markdown 是抽取后的 markdown（UTF-8，末尾统一单个换行）。
	Markdown []byte
	// Title 优先取 readability 判定的标题，缺失时回退 <title> 文本。
	Title string
	// Warnings 承载非致命告警码（WarningReadabilityFallback 等），不含敏感数据。
	Warnings []string
}

// 可读性实现为 codeberg.org/readeck/go-readability/v2（上游 go-shiori/go-readability
// 已在 go.mod 中标记 deprecated 并指向该分支），Parser 字段与其前身同名。
//
// Extract 按 spec §7 抽取正文：
//
//	HTML → (可选 target_selector 圈定) → readability（参数化 Parser）→
//	article HTML → html-to-markdown → markdown
//
// 语义要点：
//   - extract.mode=full 跳过 readability，直接对整页（或圈定区）转 markdown；
//     mode 为空或其它取值按 readability 处理（与 T3 解析器的缺省一致）；
//   - readability 产物为空或低于 char_threshold 时，若 fallback_to_full 为真则
//     回落整页并记 warning readability_fallback，否则返回 readability 的最佳尝试
//     （可能为空），由调用方按 auto_escalate.min_text_chars 判定是否升级；
//   - target_selector 未命中时记 warning target_selector_no_match 并按整页处理；
//   - pageURL 用于把相对链接绝对化（readability 的 fixRelativeURIs 与
//     html-to-markdown 的 domain 兜底），可为 nil（此时相对链接保持原样）；
//   - 入参 HTML 必须是 UTF-8（http 引擎已按响应声明字符集转换，见 fetch_http.go）。
//
// 抽取产物由调用方落 FetchedDocument.Markdown，并进入既有 max_content_bytes 双检与
// hash 流程（本函数不做长度限制：内容上限由抓取层与 application 层负责）。
func Extract(rawHTML []byte, pageURL *url.URL, cfg value.WebSourceExtractConfig) (ExtractResult, error) {
	var result ExtractResult
	if len(bytes.TrimSpace(rawHTML)) == 0 {
		return result, nil
	}

	document, err := html.Parse(bytes.NewReader(rawHTML))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("解析 HTML 失败: %w", err)
	}
	result.Title = documentTitle(document)

	scopedHTML := string(rawHTML)
	scopedIsFragment := false
	if selector := strings.TrimSpace(cfg.TargetSelector); selector != "" {
		if scoped, ok := scopeHTML(document, selector); ok {
			scopedHTML = scoped
			scopedIsFragment = true
		} else {
			result.Warnings = append(result.Warnings, WarningTargetSelectorNoMatch)
		}
	}

	if cfg.Mode == value.WebSourceExtractModeFull {
		markdown, err := toMarkdown(scopedHTML, pageURL)
		if err != nil {
			return ExtractResult{}, fmt.Errorf("整页转 markdown 失败: %w", err)
		}
		result.Markdown = markdown
		return result, nil
	}

	// 整页输入已是完整文档（保留 head 元数据供 readability 取标题/作者/时间）；
	// 只有圈定后的片段才需要补文档外壳。
	readabilityInput := scopedHTML
	if scopedIsFragment {
		readabilityInput = wrapDocument(result.Title, scopedHTML)
	}

	parser := newReadabilityParser(cfg.Readability)
	article, parseErr := parser.Parse(strings.NewReader(readabilityInput), pageURL)
	if parseErr == nil {
		markdown, textLen, err := renderReadabilityArticle(article, pageURL)
		if err != nil {
			return ExtractResult{}, err
		}
		if len(markdown) > 0 && (cfg.Readability.CharThreshold <= 0 || textLen >= cfg.Readability.CharThreshold) {
			return fillFromReadability(result, article, markdown), nil
		}
		if !cfg.FallbackToFull {
			// 不回退整页：返回 readability 的最佳尝试（可能为空），是否升级由调用方判定。
			return fillFromReadability(result, article, markdown), nil
		}
	} else if !cfg.FallbackToFull {
		return result, fmt.Errorf("readability 抽取失败: %w", parseErr)
	}

	result.Warnings = append(result.Warnings, WarningReadabilityFallback)
	markdown, err := toMarkdown(scopedHTML, pageURL)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("整页转 markdown 失败: %w", err)
	}
	result.Markdown = markdown
	return result, nil
}

// fillFromReadability 用 readability 产物补齐结果（正文与标题）。
func fillFromReadability(result ExtractResult, article readability.Article, markdown []byte) ExtractResult {
	if title := strings.TrimSpace(article.Title()); title != "" {
		result.Title = title
	}
	result.Markdown = markdown
	return result
}

// renderReadabilityArticle 把 readability 产物渲染为 markdown 并统计正文字符数。
//
// 该实现（codeberg.org/readeck/go-readability/v2）的 Article 不再是「Content 字符串 +
// Length 计数」的形态，而是暴露 Node（正文容器）与 RenderHTML/RenderText；因此这里
// 显式渲染两遍：HTML 交给 html-to-markdown，纯文本只用于与 char_threshold 比对口径
// （折叠空白后按 rune 计数，与库内 CharThresholds 的字符口径一致）。
func renderReadabilityArticle(article readability.Article, pageURL *url.URL) ([]byte, int, error) {
	if article.Node == nil {
		return nil, 0, nil
	}
	var htmlBuilder strings.Builder
	if err := article.RenderHTML(&htmlBuilder); err != nil {
		return nil, 0, fmt.Errorf("渲染 readability 正文 HTML 失败: %w", err)
	}
	markdown, err := toMarkdown(htmlBuilder.String(), pageURL)
	if err != nil {
		return nil, 0, fmt.Errorf("readability 产物转 markdown 失败: %w", err)
	}
	var textBuilder strings.Builder
	if err := article.RenderText(&textBuilder); err != nil {
		return nil, 0, fmt.Errorf("渲染 readability 正文文本失败: %w", err)
	}
	return markdown, runeCount(collapseSpaces(textBuilder.String())), nil
}

// newReadabilityParser 把 extract.readability 映射为 readability Parser。
//
// 映射说明（键名与库字段并不总是一致，逐项对齐）：
//   - char_threshold → CharThresholds（未达标时库内自身会按重试语义再试，spec §7）；
//   - nb_top_candidates → NTopCandidates（库字段名无 "b"）；
//   - classes_to_preserve / keep_classes → 同名字段；
//   - link_density_modifier 已从配置合同移除（所选实现在该版本无此字段），
//     以免形成「配了不生效」的陷阱（spec §7）。
func newReadabilityParser(cfg value.WebSourceReadabilityConfig) readability.Parser {
	parser := readability.NewParser()
	if cfg.CharThreshold > 0 {
		parser.CharThresholds = cfg.CharThreshold
	}
	if cfg.NbTopCandidates > 0 {
		parser.NTopCandidates = cfg.NbTopCandidates
	}
	if len(cfg.ClassesToPreserve) > 0 {
		parser.ClassesToPreserve = cfg.ClassesToPreserve
	}
	parser.KeepClasses = cfg.KeepClasses
	return parser
}

// documentTitle 返回首个 <title> 的文本（已由解析器实体解码），缺失返回空串。
func documentTitle(document *html.Node) string {
	title := findElement(document, "title")
	if title == nil {
		return ""
	}
	return collapseSpaces(collectText(title, nil))
}

// scopeHTML 用 CSS 选择器圈定抽取范围：返回全部命中节点的序列化 HTML
// （多命中时拼接，覆盖「正文被拆成多个容器」的站点）。选择器非法或未命中返回 ok=false。
func scopeHTML(document *html.Node, selector string) (string, bool) {
	matcher, err := cascadia.Compile(selector)
	if err != nil {
		return "", false
	}
	nodes := cascadia.QueryAll(document, matcher)
	if len(nodes) == 0 {
		return "", false
	}
	var builder strings.Builder
	for _, node := range nodes {
		if err := html.Render(&builder, node); err != nil {
			return "", false
		}
	}
	return builder.String(), true
}

// wrapDocument 把 HTML 片段包成完整文档：readability 期望完整文档语义
// （依赖 body/head 结构与 <title> 元数据），圈定后的片段需要补齐外壳。
func wrapDocument(title, fragment string) string {
	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html><html><head><title>")
	builder.WriteString(html.EscapeString(title))
	builder.WriteString("</title></head><body>")
	builder.WriteString(fragment)
	builder.WriteString("</body></html>")
	return builder.String()
}

// toMarkdown 把 HTML 转 markdown：pageURL 非空时把相对链接解析为绝对链接，
// 使下游资产归档（AssetResolver）能下载图片。空产物返回 nil。
func toMarkdown(fragmentHTML string, pageURL *url.URL) ([]byte, error) {
	var options []converter.ConvertOptionFunc
	if pageURL != nil {
		options = append(options, converter.WithDomain(pageURL.String()))
	}
	markdown, err := htmltomarkdown.ConvertString(fragmentHTML, options...)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(markdown, "\n")
	if strings.TrimSpace(trimmed) == "" {
		return nil, nil
	}
	return []byte(trimmed + "\n"), nil
}
