package web

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// 本文件除 JS 空壳判定外，同时承载包内共用的 HTML 遍历辅助
//（findElementsByTag/findElementsByID/collectText/collapseSpaces/runeCount），
// 供抽取层与抓取层复用；这些辅助的修改原因与遍历语义一致，故不另立 utils 文件。

// jsShellMaxVisibleTextChars 是 JS 空壳判定的可见文本阈值（spec §6.2）。
// 该阈值不在 web 源配置合同（kb_sources.config）内，故为包内具名常量而非配置项。
const jsShellMaxVisibleTextChars = 200

// jsShellMountPointIDs 是常见前端框架的空挂载点 id（spec §6.2）。
// HTML id 大小写敏感，故按原样精确匹配。
var jsShellMountPointIDs = []string{"root", "app", "__next"}

// invisibleTextTags 是不渲染为可见文本的标签（其内容不计入 body 可见文本）。
var invisibleTextTags = map[string]struct{}{
	"script":   {},
	"style":    {},
	"noscript": {},
	"template": {},
}

// noscriptInnerSkipTags 是统计 <noscript> 文本时需继续排除的标签。
var noscriptInnerSkipTags = map[string]struct{}{
	"script":   {},
	"style":    {},
	"template": {},
}

// isLikelyJSShell 判定页面是否像 JS 空壳（spec §6.2 纯函数）。
// 升级动作（用 connection 指向的引擎重抓）由 T11 编排，本函数只做判定。
//
// 判定规则（满足其一即命中）：
//   - body 可见文本字符数 < 阈值，且存在空挂载点 id="root|app|__next"；
//   - body 可见文本字符数 < 阈值，且正文几乎全在 <noscript> 中
//     （noscript 文本字符数 >= 阈值）。
//
// extractedLen 是本页已抽取正文的字符数（rune 数，与 auto_escalate.min_text_chars
// 同一口径）：已达到阈值说明抽取拿到了正文，不再判定为空壳。
// HTML 不可解析时返回 false——宁可不升级，也不误升级。
func isLikelyJSShell(rawHTML []byte, extractedLen int) bool {
	if extractedLen >= jsShellMaxVisibleTextChars || len(rawHTML) == 0 {
		return false
	}
	document, err := html.Parse(bytes.NewReader(rawHTML))
	if err != nil {
		return false
	}
	body := findElement(document, "body")
	if body == nil {
		return false
	}
	if runeCount(collapseSpaces(collectText(body, invisibleTextTags))) >= jsShellMaxVisibleTextChars {
		return false
	}
	if hasEmptyMountPoint(body) {
		return true
	}
	return noscriptTextRunes(body) >= jsShellMaxVisibleTextChars
}

// hasEmptyMountPoint 报告 body 内是否存在空的框架挂载点。
func hasEmptyMountPoint(body *html.Node) bool {
	for _, id := range jsShellMountPointIDs {
		for _, node := range findElementsByID(body, id) {
			if runeCount(collapseSpaces(collectText(node, invisibleTextTags))) == 0 {
				return true
			}
		}
	}
	return false
}

// noscriptTextRunes 统计 body 内所有 <noscript> 的可见文本字符数。
func noscriptTextRunes(body *html.Node) int {
	total := 0
	for _, node := range findElementsByTag(body, "noscript") {
		total += runeCount(collapseSpaces(collectText(node, noscriptInnerSkipTags)))
	}
	return total
}

// collectText 收集子树内的文本节点内容；skip 集合内的元素及其子树不参与统计。
func collectText(node *html.Node, skip map[string]struct{}) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.ElementNode {
			if _, skipped := skip[current.Data]; skipped {
				return
			}
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

// collapseSpaces 折叠连续空白，使阈值口径不受排版空白影响。
func collapseSpaces(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func runeCount(text string) int {
	return utf8.RuneCountInString(text)
}

func findElement(root *html.Node, tag string) *html.Node {
	found := findElementsByTag(root, tag)
	if len(found) == 0 {
		return nil
	}
	return found[0]
}

// findElementsByTag 深度优先收集所有指定标签的元素。
func findElementsByTag(root *html.Node, tag string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.ElementNode && current.Data == tag {
			found = append(found, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

// findElementsByID 深度优先收集 id 属性精确等于 id 的元素。
func findElementsByID(root *html.Node, id string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.ElementNode {
			for _, attr := range current.Attr {
				if attr.Key == "id" && attr.Val == id {
					found = append(found, current)
					break
				}
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}
