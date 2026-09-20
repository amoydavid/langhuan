package web

import (
	"context"
	"unicode/utf8"
)

// auto 引擎（spec §6.2）：engine=auto 等价于 http 起步 + 命中升级条件时用
// connection 指向的引擎重抓。升级条件（满足其一）：
//   - 抽取正文字符数（rune 数）< fetch.auto_escalate.min_text_chars；
//   - JS 空壳启发式命中（jshell.go isLikelyJSShell，rune 数口径）。
//
// last_engine 短路：FetchOptions 透传该文档上次成功的引擎（source_meta.last_engine），
// 是 render/firecrawl/jina 时直接用该引擎重抓；失败回落 http + 升级判定（再判定）。
// auto_escalate.enabled=false 时 auto 退化为纯 http——短路一并停用：用户显式关闭
// 升级语义后，昂贵的重抓不应继续发生。
//
// 运行时防御路径：升级无处可去（无连接凭证/连接被删或停用/provider 不可识别）时
// 接受 http 结果 + warning possible_js_shell，不算失败——创建时 auto 必须有
// connection，运行期连接可能消失（spec §4.1/§6.2）。

// isAdvancedEngine 报告引擎标识是否属于升级目标集合（render/firecrawl/jina）。
func isAdvancedEngine(engine string) bool {
	switch engine {
	case EngineRender, EngineFirecrawl, EngineJina:
		return true
	}
	return false
}

// fetchEngine 是会话的单页抓取入口：非 auto 引擎直连；auto 引擎执行 http 起步 +
// 升级判定 + last_engine 短路（request.LastEngine 由 Fetch 侧透传，发现阶段恒空
// ——发现层尚无文档身份，走 auto 判定）。
func (s *runSession) fetchEngine(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	if s.engine == nil {
		return s.autoFetch(ctx, request)
	}
	return s.engine(ctx, request)
}

// autoFetch 执行 auto 引擎的单页抓取，返回最终页面（命中升级时被高级引擎的
// 结果替换）。升级重抓失败按该引擎的错误分类透传（retryable → Job 级重试兜底）：
// 不用疑似空壳的 http 结果顶替——空壳内容一旦建档，后续同步 hash 一致反而
// 难以纠正。
func (s *runSession) autoFetch(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	escalate := s.escalate
	// shortCircuit：上次内容出自高级引擎（短路输入有效）。enabled=false 时不短路。
	shortCircuit := s.cfg.Fetch.AutoEscalate.Enabled && isAdvancedEngine(request.LastEngine)

	// last_engine 短路（spec §6.2）：直接沿用上次成功引擎；失败回落 auto 判定。
	if shortCircuit && escalate != nil {
		page, err := escalate(ctx, request)
		if err == nil {
			return page, nil
		}
	}

	// http 起步。上次内容出自高级引擎时剥离条件请求头：etag/last_modified 是
	// http 通道观测的原文快照凭据，与文档实际内容（渲染/markdown 直通）无对应
	// 关系——304 会把「渲染内容可能已变化」误判为未变更；短路失败回落正是需要
	// 重新取内容再判定的场景。
	httpRequest := request
	if shortCircuit {
		httpRequest.IfNoneMatch = ""
		httpRequest.IfModifiedSince = ""
	}
	page, err := s.httpFetch(ctx, httpRequest)
	if err != nil || page.NotModified {
		return page, err
	}
	if !s.cfg.Fetch.AutoEscalate.Enabled {
		return page, nil
	}

	// 升级判定：抽取一次正文（产物缓存在页上，组装阶段不二次抽取）。
	// 抽取失败不在此处升级：组装阶段 extractPage 会复现同一错误并按既有语义
	// 消费——宁可不升级，不掩盖错误。
	extracted, extractErr := s.extractForEscalationCheck(page, request.URL)
	if extractErr != nil {
		return page, nil
	}
	extractedLen := utf8.RuneCount(extracted.Markdown)
	if extractedLen >= s.cfg.Fetch.AutoEscalate.MinTextChars && !isLikelyJSShell(page.HTML, extractedLen) {
		page.extracted = &extracted
		return page, nil
	}

	// 升级无处可去：接受原结果 + warning possible_js_shell（运行时防御路径）。
	if escalate == nil {
		page.extracted = &extracted
		page.Warnings = append(page.Warnings, WarningPossibleJSShell)
		return page, nil
	}

	escalated, err := escalate(ctx, request)
	if err != nil {
		return fetchedPage{}, err
	}
	return escalated, nil
}

// extractForEscalationCheck 为升级判定抽取正文；base 语义与 extractPage 一致
// （重定向后的 FinalURL 优先，请求 URL 兜底）。
func (s *runSession) extractForEscalationCheck(page fetchedPage, requestURL string) (ExtractResult, error) {
	base, ok := resolveToURL(nil, page.FinalURL)
	if !ok {
		base, _ = resolveToURL(nil, requestURL)
	}
	return Extract(page.HTML, base, s.cfg.Extract)
}
