package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件承载「引擎合同」：所有引擎（http/render/firecrawl/jina）对同一 URL 返回的
// 统一内部结果（spec §6.1）与失败分类词汇（spec §6.5）。抓取/重试/限速等引擎内部
// 实现见 fetch_http.go 与后续的 fetch_render.go / fetch_provider_*.go。

// EngineHTTP 是 http 引擎标识（写入 documents.source_meta.last_engine，spec §4.3）。
const EngineHTTP = "http"

// 其余引擎标识（与 value.WebSourceFetchEngine* 取值一致；last_engine 留痕与
// auto 短路判定按这些字面量比较，spec §6.1/§6.2）。
const (
	EngineRender    = "render"
	EngineFirecrawl = "firecrawl"
	EngineJina      = "jina"
)

// fetchedPage 是所有引擎对同一 URL 的统一内部结果（spec §6.1）。
// http/render 通道填 HTML（Markdown 由 §7 抽取层生成）；provider 通道直出 Markdown。
// 该类型是包内合同：T6（发现）、T7（连接器总装）、T11（高级引擎）共同消费。
type fetchedPage struct {
	// FinalURL 是重定向归并后的最终 URL。
	FinalURL string
	// Status 是 HTTP 状态；provider 引擎映射为等价语义。
	Status int
	// HTML 是响应体（已按声明字符集转为 UTF-8）；provider 通道可空。
	HTML []byte
	// Markdown 是 provider 通道直出内容；http/render 通道由抽取层填充。
	Markdown []byte
	// Links 是发现用出链的原始 href 集合（未解析、未过滤）。
	Links []string
	// ETag/LastModified 是响应头原值，用于下次条件请求（仅 http 引擎）。
	ETag         string
	LastModified string
	// Engine 是实际执行引擎。
	Engine string
	// NotModified 表示条件请求命中 304：无正文，调用方按「未变更」处理。
	NotModified bool
	// Warnings 是本页抓取过程的非致命告警码（如 possible_js_shell），
	// 由 fetchDocument 并入 FetchedDocument.Warnings 透出。
	Warnings []string
	// extracted 缓存 auto 引擎升级判定时已完成的抽取产物，使同一页在
	// fetchDocument 组装阶段不二次跑抽取管线；nil 表示未缓存（由调用方现算）。
	extracted *ExtractResult
}

// PageOutcome 是单页抓取结果分类（spec §6.5），供编排层决定
// 「入 ToRemove / 跳过 + warning / 走 RetryRequired」。
type PageOutcome string

const (
	// OutcomeOK 表示可取用该页（2xx/3xx，含 304 未变更）。
	OutcomeOK PageOutcome = "ok"
	// OutcomeGone 表示 404/410：远端已删除。
	OutcomeGone PageOutcome = "gone"
	// OutcomeBlocked 表示 401/403 认证墙：跳过 + warning，不算 retryable。
	OutcomeBlocked PageOutcome = "blocked"
	// OutcomeSkipped 表示其它不可恢复的 4xx（400/405/406/422 等）：跳过 + warning。
	OutcomeSkipped PageOutcome = "skipped"
	// OutcomeRetryable 表示网络错误、408/425/429、5xx、超时：走 Job 级 RetryRequired。
	OutcomeRetryable PageOutcome = "retryable"
	// OutcomeOversize 表示内容超过 max_content_bytes：同样走 RetryRequired，
	// 但需保留 oversize 语义（ErrSourceContentTooLarge）。
	OutcomeOversize PageOutcome = "oversize"
	// OutcomeRobotsDisallowed 表示 robots.txt 禁止抓取：跳过 + warning，不算 retryable。
	// 由 RobotsChecker 判定产生，不与 ClassifyStatus/ClassifyError 的输入重叠。
	OutcomeRobotsDisallowed PageOutcome = "robots_disallowed"
	// OutcomeSkippedNonHTML 表示响应内容类型不是 HTML（PDF/DOC/图片等，spec §1/§6.5/§7）：
	// 不抽取、不进 Nodes，记 warning skipped_non_html，不算 retryable。
	// 注意此时 Status 可能是 200，编排层必须按 Outcome（而非状态码）判定。
	OutcomeSkippedNonHTML PageOutcome = "skipped_non_html"
)

// 告警码（写入 TreeSnapshot.Warnings / Job 结果）。
const (
	// WarningSkippedNonHTML 表示该页因内容类型不可抽取被跳过；
	// 与 OutcomeSkippedNonHTML 同一字面量（outcome 供编排层分类，warning 供结果透出）。
	WarningSkippedNonHTML = "skipped_non_html"
	// WarningPossibleJSShell 表示 auto 引擎命中升级条件但升级无处可去
	//（无 connection 或连接不可用，运行时防御路径，spec §6.2）：
	// 已接受 http 结果（疑似 JS 空壳），不算失败。
	WarningPossibleJSShell = "possible_js_shell"
)

// ClassifyStatus 按 HTTP 状态码判定单页结果分类。status=0 表示未取得响应。
func ClassifyStatus(status int) PageOutcome {
	switch {
	case status == 0:
		return OutcomeRetryable
	case status >= 200 && status < 400:
		return OutcomeOK
	case status == http.StatusNotFound || status == http.StatusGone:
		return OutcomeGone
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return OutcomeBlocked
	case status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests:
		return OutcomeRetryable
	case status >= 500:
		return OutcomeRetryable
	default:
		return OutcomeSkipped
	}
}

// ClassifyError 按错误判定单页结果分类，覆盖 http 引擎与 provider 引擎
// （T8 把端口层错误也交给本函数统一归类）。
func ClassifyError(err error) PageOutcome {
	if err == nil {
		return OutcomeOK
	}
	var pageErr *PageError
	if errors.As(err, &pageErr) && pageErr.Outcome != "" {
		return pageErr.Outcome
	}
	switch {
	case errors.Is(err, sourceport.ErrSourceContentTooLarge):
		return OutcomeOversize
	case errors.Is(err, sourceport.ErrSourceNotFound):
		return OutcomeGone
	case errors.Is(err, sourceport.ErrSourceUnavailable):
		return OutcomeRetryable
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return OutcomeRetryable
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return OutcomeRetryable
	}
	return OutcomeRetryable
}

// PageError 是单页抓取失败，携带 HTTP 状态与分类，供编排层映射为
// ToRemove / warning / RetryRequired（spec §6.5）。
type PageError struct {
	// Status 是 HTTP 状态；0 表示未取得响应（网络错误/超时/超限）。
	Status int
	// Outcome 是该失败的结果分类（绝不为 OutcomeOK）。
	Outcome PageOutcome
	// Err 是底层错误，支持 errors.Is/As 链。
	Err error
}

func (e *PageError) Error() string {
	return fmt.Sprintf("抓取页面失败 (outcome=%s status=%d): %v", e.Outcome, e.Status, e.Err)
}

func (e *PageError) Unwrap() error { return e.Err }
