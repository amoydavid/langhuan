package web

import (
	"context"
	"fmt"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件实现 web connector 的 Fetch（spec §3.2/§6/§7）：robots 门、本次运行页缓存、
// 引擎分派、正文抽取与 FetchedDocument/source_meta 组装。

// Fetch 拉取单个页面并抽取为 markdown。
//
//   - externalID 是页面身份（规范化 URL，spec §3.2/§3.3）；这里做幂等规范化复核，
//     非法值按 ErrSourceNotFound 处理（与 feishu「对象不存在」同分类）。
//   - 命中本次运行页缓存（crawl 发现阶段已抓）时不发真实请求；缓存复用优先于条件
//     请求——内容是否变化由调用方 hash 兜底（spec §8 第三级）。
//   - 条件请求头（IfNoneMatch/IfModifiedSince）原样透传给 http 引擎；304 返回
//     NotModified=true 的空正文文档，Meta 只含本次观测到的键（last_fetch_at 等供
//     调用方合并回写，spec §8「刷新 last_fetch_at、不建 revision」）。
//   - 非 HTML 内容类型：返回 *PageError{Outcome: skipped_non_html}——编排层用
//     web.ClassifyError 归类为「跳过 + warning、不算 retryable」（spec §6.5），
//     与「跳过不进 Nodes」对齐；本 connector 不为它伪造空文档。
//   - MaxContentBytes 双检的第一道在抓取层（响应体上限）与抽取后（这里，与 feishu
//     connector 同款），第二道在 application 层（spec §7）。
func (c *Connector) Fetch(ctx context.Context, _ model.SourceConnection, externalID string, options sourceport.FetchOptions) (model.FetchedDocument, error) {
	if externalID == "" {
		return model.FetchedDocument{}, fmt.Errorf("%w: external_id 为空", sourceport.ErrSourceNotFound)
	}
	normalized, ok := NormalizeURL(externalID)
	if !ok {
		return model.FetchedDocument{}, fmt.Errorf("%w: external_id 不是合法的绝对 HTTP(S) URL: %q", sourceport.ErrSourceNotFound, externalID)
	}
	// 复用 run 会话（缓存/robots/限速状态）；Fetch 前无 ListTree 时惰性建一次性会话。
	session, err := c.sessionFor(options.SourceConfig, false)
	if err != nil {
		return model.FetchedDocument{}, err
	}
	return session.fetchDocument(ctx, normalized, options)
}

// fetchDocument 执行单页拉取与组装。错误通道只有两类：*PageError（引擎/robots，
// 编排层按分类消费）与包装端口哨兵的普通错误（ErrSourceContentTooLarge、ctx 中断）。
func (s *runSession) fetchDocument(ctx context.Context, pageURL string, options sourceport.FetchOptions) (model.FetchedDocument, error) {
	var warnings []string
	if s.robots != nil {
		allowed, warning, err := s.robots.Allowed(ctx, pageURL)
		if err != nil {
			// ctx 中断/目标非法：ClassifyError 归 retryable，由 Job 级重试兜底。
			return model.FetchedDocument{}, fmt.Errorf("robots 校验中断: %w", err)
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if !allowed {
			// 发现阶段已过滤 robots 禁页，这里是独立 Fetch 路径的同一道门（spec §6.3：
			// 抓取动作前都查 robots）；不算 retryable。
			return model.FetchedDocument{}, &PageError{
				Outcome: OutcomeRobotsDisallowed,
				Err:     fmt.Errorf("robots.txt 禁止抓取: %s", pageURL),
			}
		}
	}

	page, cacheHit := s.cache.Get(pageURL)
	if !cacheHit {
		fetched, err := s.fetchEngine(ctx, FetchRequest{
			URL:             pageURL,
			IfNoneMatch:     options.IfNoneMatch,
			IfModifiedSince: options.IfModifiedSince,
			MaxContentBytes: options.MaxContentBytes,
			LastEngine:      options.LastEngine,
		})
		if err != nil {
			// *PageError 原样透传：分类词汇（gone/blocked/retryable/oversize/
			// skipped_non_html/robots_disallowed）在 fetch_contract.go，编排层据此
			// 映射 ToRemove / 跳过+warning / RetryRequired（spec §6.5）。
			return model.FetchedDocument{}, err
		}
		page = fetched
		// 304 结果不入缓存：缓存值必须是「可复用的完整页」，否则同一 run 内后续
		// 不带条件头的 Fetch 会拿到没有正文的 NotModified 结果。
		if !page.NotModified {
			s.cache.Put(pageURL, page)
		}
	}

	if page.NotModified {
		// 304 未发生内容处理：不写 last_engine（meta 合并保留旧值），避免 auto 源
		// 的 last_engine 短路提示被一次 304 冲回 http。
		return model.FetchedDocument{
			ObjType:     ObjTypePage,
			NotModified: true,
			Meta: sourceport.SourceMeta{
				LastFetchAt:    time.Now(),
				LastHTTPStatus: page.Status,
				FinalURL:       page.FinalURL,
			}.ToMap(),
			Warnings: warnings,
		}, nil
	}

	// 缓存命中的页不经过本次条件头语境：直接复用完整内容，变更检测由调用方
	// hash 兜底（spec §5.1 发现-抓取复用优先）。
	warnings = append(warnings, page.Warnings...)
	extracted, err := s.extractPage(page, pageURL)
	if err != nil {
		return model.FetchedDocument{}, err
	}
	if options.MaxContentBytes > 0 && int64(len(extracted.Markdown)) > options.MaxContentBytes {
		return model.FetchedDocument{}, fmt.Errorf("%w: 页面 %s 抽取后内容 %d 字节超过上限 %d",
			sourceport.ErrSourceContentTooLarge, pageURL, len(extracted.Markdown), options.MaxContentBytes)
	}
	warnings = append(warnings, extracted.Warnings...)
	return model.FetchedDocument{
		Markdown: extracted.Markdown,
		Title:    extracted.Title,
		ObjType:  ObjTypePage,
		Meta: sourceport.SourceMeta{
			ETag:           page.ETag,
			LastModified:   page.LastModified,
			LastEngine:     page.Engine,
			LastFetchAt:    time.Now(),
			LastHTTPStatus: page.Status,
			FinalURL:       page.FinalURL,
		}.ToMap(),
		Warnings: warnings,
	}, nil
}

// extractPage 抽取正文：http 通道对 HTML 跑抽取管线（extract.go）；provider 通道
// markdown 直出（不跑本地抽取，extract.mode 已映射为 provider 侧等价选项）。auto
// 引擎升级判定时缓存的抽取产物（page.extracted）直接复用，避免二次跑管线。
// 抽取的相对链接以重定向后的 FinalURL 为绝对化 base（下游 AssetResolver 依赖绝对链接）。
func (s *runSession) extractPage(page fetchedPage, requestURL string) (ExtractResult, error) {
	if page.extracted != nil {
		return *page.extracted, nil
	}
	if len(page.Markdown) > 0 {
		return ExtractResult{Markdown: page.Markdown}, nil
	}
	base, ok := resolveToURL(nil, page.FinalURL)
	if !ok {
		// FinalURL 必可解析（来自 http.Response），此处兜底请求 URL；两者皆失败时
		// base 为 nil，Extract 容忍 nil（相对链接保持原样）。
		base, _ = resolveToURL(nil, requestURL)
	}
	return Extract(page.HTML, base, s.cfg.Extract)
}
