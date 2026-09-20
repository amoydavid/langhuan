package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"

	"github.com/dajee/langhuan/internal/adapters/httpclient"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// render 引擎（spec §6.1/§6.4）：go-rod 连外部 CDP（browser connection 的
// endpoint_url + 可选 token）→ navigate → WaitStable → outerHTML。
//
// SSRF 降级语义（spec §6.4，必须如实理解）：浏览器进程侧的子请求与 DNS 解析
// 发生在渲染端，本进程的 dial 防护不生效——琅嬛侧能做的是导航前解析目标 host
// 并拒绝私网地址（ValidateWebTarget）；端到端保证依赖渲染端能力或部署侧网络
// 隔离，文档明示（T18）。
//
// 并发：单页渲染受进程级信号量（web_source.render_max_concurrent）约束，
// 信号量挂在 Connector 上跨源共享，见 newRenderLimiter。
//
// 测试策略：真实 CDP 需要浏览器，单测经 pageNavigator 接口注入 fake 覆盖成功
// 路径/私网拒绝/信号量上限，dial 注入覆盖 CDP 不可达；真实渲染不过度测试
//（集成阶段人工验收）。

const (
	// defaultRenderMaxConcurrent 是 render_max_concurrent 未配置时的默认并发上限
	//（spec §4.2 示例值）。
	defaultRenderMaxConcurrent = 2
	// defaultRenderTimeout 是单页渲染超时上限（生产由 web_source.request_timeout_seconds
	// 注入 ConnectorConfig.HTTPClient.Timeout 推导），覆盖 CDP 连接建立（dial/握手）
	// 与页面渲染全程；防止异常页面或黑洞式渲染端点长时间占用渲染槽。
	defaultRenderTimeout = 30 * time.Second
	// defaultRenderStableWindow 是渲染稳定判定的静默窗口：两个窗口内 DOM 尺寸
	// 无变化即认为渲染完成（与 go-rod 文档建议同量级）。只约束「DOM 稳定采样」
	// 阶段；「页面加载」阶段（readyState→complete）不受它约束，跟随抓取请求
	// 的 ctx（生产为 request_timeout_seconds，30s 量级）——真实公网页面的 load
	// 中位数在秒级，压进静默窗口会把慢页面错杀为渲染失败（2026-09-18 评审）。
	defaultRenderStableWindow = 500 * time.Millisecond
)

// RenderCDPEndpoint 拼装 CDP 连接端点：token 非空时以 query 参数附加
// （browserless 系部署的通行约定；已有 query 则追加）。T12 的连接测试握手
// （Browser.getVersion）应复用同一拼装，保证「测试可用」与「运行时可用」一致。
func RenderCDPEndpoint(endpointURL, token string) string {
	endpointURL = strings.TrimSpace(endpointURL)
	if token == "" {
		return endpointURL
	}
	separator := "?"
	if strings.Contains(endpointURL, "?") {
		separator = "&"
	}
	return endpointURL + separator + "token=" + url.QueryEscape(token)
}

// renderResult 是一次渲染导航的产物。
type renderResult struct {
	// FinalURL 是渲染后的地址栏 URL（重定向归并后）。
	FinalURL string
	// HTML 是页面 outerHTML（浏览器序列化产物，必为 HTML）。
	HTML string
}

// pageNavigator 抽象渲染浏览器的导航生命周期，使 render 引擎的抓取行为可脱离
// 真实 CDP 测试。实现必须保证：Navigate 后页面已完成加载稳定（等价 WaitStable
// 语义），返回的 HTML 为渲染后的 outerHTML。
type pageNavigator interface {
	// Navigate 渲染 pageURL 并返回产物；ctx 取消/超时必须中断渲染并返回错误。
	Navigate(ctx context.Context, pageURL string) (renderResult, error)
	// Close 释放浏览器连接；重复调用必须安全。
	Close() error
}

// renderLimiter 是 render 引擎的进程级并发信号量（web_source.render_max_concurrent，
// spec §4.2）：同一进程内所有源的渲染页共享上限，防止 JS 渲染挤占宿主资源。
type renderLimiter struct {
	sem chan struct{}
}

func newRenderLimiter(maxConcurrent int) *renderLimiter {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultRenderMaxConcurrent
	}
	return &renderLimiter{sem: make(chan struct{}, maxConcurrent)}
}

// acquire 取得一个渲染槽；ctx 取消时放弃等待。返回的释放函数必须调用一次。
func (l *renderLimiter) acquire(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case l.sem <- struct{}{}:
		return func() { <-l.sem }, nil
	}
}

// RenderFetcherConfig 配置 render 引擎抓取器。
type RenderFetcherConfig struct {
	// CDPURL 是渲染端点（RenderCDPEndpoint 的产物）；空值视为装配错误。
	CDPURL string
	// UserAgent 是渲染 UA（会话 UA：user_agent_template 或源级 crawl.user_agent）；
	// 空值保留浏览器默认 UA。
	UserAgent string
	// Limiter 是进程级渲染信号量；nil 时本抓取器独立限额（默认值）。
	Limiter *renderLimiter
	// Timeout 是单页渲染超时；<=0 用 defaultRenderTimeout。
	Timeout time.Duration
	// MaxContentBytes 是渲染产物上限；<=0 表示不限（请求级 MaxContentBytes 优先）。
	MaxContentBytes int64
	// dial 可注入（测试）；nil 用 newRodNavigator（go-rod 连 CDP）。timeout 为
	// 单页渲染超时（覆盖连接建立阶段，由实现自行施加）。
	dial func(ctx context.Context, cdpURL string, timeout time.Duration) (pageNavigator, error)
	// validateTarget 可注入（测试绕过真实 DNS 预校验）；nil 用
	// httpclient.ValidateWebTarget（导航前公网校验，spec §6.4）。
	validateTarget func(ctx context.Context, rawURL string) error
}

// RenderFetcher 是 render 引擎的单页抓取器。
//
// 浏览器连接惰性建立并跨页复用（一次会话一个 CDP 连接）；连接生命周期由
// Close 控制——会话被替换时 connector 负责调用（见 connector.go sessionFor）。
// Close 后再 Fetch 返回错误，不再重建连接。
type RenderFetcher struct {
	cdpURL          string
	userAgent       string
	timeout         time.Duration
	maxContentBytes int64
	limiter         *renderLimiter
	dial            func(ctx context.Context, cdpURL string, timeout time.Duration) (pageNavigator, error)
	validateTarget  func(ctx context.Context, rawURL string) error

	mu     sync.Mutex
	nav    pageNavigator
	closed bool
}

// NewRenderFetcher 构造 render 引擎抓取器；CDP endpoint 缺失视为装配错误。
func NewRenderFetcher(cfg RenderFetcherConfig) (*RenderFetcher, error) {
	if strings.TrimSpace(cfg.CDPURL) == "" {
		return nil, fmt.Errorf("render 引擎必须提供 CDP endpoint_url（browser connection config）")
	}
	dial := cfg.dial
	if dial == nil {
		dial = func(ctx context.Context, cdpURL string, timeout time.Duration) (pageNavigator, error) {
			return newRodNavigator(ctx, cdpURL, timeout)
		}
	}
	validateTarget := cfg.validateTarget
	if validateTarget == nil {
		validateTarget = httpclient.ValidateWebTarget
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultRenderTimeout
	}
	limiter := cfg.Limiter
	if limiter == nil {
		limiter = newRenderLimiter(0)
	}
	return &RenderFetcher{
		cdpURL:          cfg.CDPURL,
		userAgent:       cfg.UserAgent,
		timeout:         timeout,
		maxContentBytes: cfg.MaxContentBytes,
		limiter:         limiter,
		dial:            dial,
		validateTarget:  validateTarget,
	}, nil
}

// Fetch 渲染单页并返回统一内部结果。render 通道不支持条件请求
// （request 的 IfNoneMatch/IfModifiedSince 被忽略，spec §8 条件请求仅 http 引擎）。
func (f *RenderFetcher) Fetch(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	// 导航前琅嬛侧 host 预校验（spec §6.4 降级语义）：私网/环回/链路本地目标
	// 在发起渲染前拒绝，属「跳过 + warning」类不可恢复失败。
	if err := f.validateTarget(ctx, request.URL); err != nil {
		return fetchedPage{}, &PageError{
			Outcome: OutcomeSkipped,
			Err:     fmt.Errorf("渲染目标未通过公网预校验: %w", err),
		}
	}
	maxBytes := request.MaxContentBytes
	if maxBytes <= 0 {
		maxBytes = f.maxContentBytes
	}

	// 渲染槽：进程级上限，等待计入 ctx（调度层超时可打断排队）。
	release, err := f.limiter.acquire(ctx)
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: fmt.Errorf("等待渲染槽被中断: %w", err)}
	}
	defer release()

	nav, err := f.navigator(ctx)
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: err}
	}

	renderCtx := ctx
	if f.timeout > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, f.timeout)
		defer cancel()
		renderCtx = timeoutCtx
	}
	result, err := nav.Navigate(renderCtx, request.URL)
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: fmt.Errorf("渲染页面失败: %w", err)}
	}

	html := []byte(result.HTML)
	if maxBytes > 0 && int64(len(html)) > maxBytes {
		return fetchedPage{}, &PageError{
			Status:  http.StatusOK,
			Outcome: OutcomeOversize,
			Err: fmt.Errorf("%w: 渲染页面 outerHTML %d 字节超过上限 %d",
				sourceport.ErrSourceContentTooLarge, len(html), maxBytes),
		}
	}
	finalURL := result.FinalURL
	if finalURL == "" {
		finalURL = request.URL
	}
	return fetchedPage{
		FinalURL: finalURL,
		Status:   http.StatusOK,
		HTML:     html,
		Links:    extractHrefs(html),
		Engine:   EngineRender,
	}, nil
}

// Close 释放底层浏览器连接；重复调用安全。
func (f *RenderFetcher) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	if f.nav != nil {
		_ = f.nav.Close()
		f.nav = nil
	}
}

// navigator 返回（必要时惰性建立）浏览器连接。连接建立受 mu 保护：同一抓取器
// 的并发页共享一个 CDP 连接；建立期间其它同实例抓取短暂排队（信号量保证全局
// 并发有限，排队有界）。
func (f *RenderFetcher) navigator(ctx context.Context) (pageNavigator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, fmt.Errorf("render 抓取器已关闭")
	}
	if f.nav == nil {
		nav, err := f.dial(ctx, f.cdpURL, f.timeout)
		if err != nil {
			return nil, fmt.Errorf("连接渲染浏览器失败: %w", err)
		}
		f.nav = nav
	}
	return f.nav, nil
}

// rodNavigator 是 pageNavigator 的 go-rod 实现：一个 CDP 连接上逐页
// 打开目标 → （可选 UA 覆盖）→ navigate → WaitStable → outerHTML。
type rodNavigator struct {
	// ctx 是浏览器连接的生命周期 ctx（由 Close 终止）。刻意不绑单次抓取的
	// ctx：否则首个抓取请求结束后连接随 ctx 失效，后续页全部失败。单页超时
	// 在 Navigate 内经 page.Context 施加。
	ctx     context.Context
	cancel  context.CancelFunc
	browser *rod.Browser
	ws      *cdp.WebSocket

	userAgent    string
	stableWindow time.Duration
}

// sanitizeCDPEndpoint 返回可进错误消息/日志的端点形态：剥离 userinfo、query
// 与 fragment（token 以 query 附加，绝不能随 go-rod 内嵌的 URL 进入错误链），
// 解析失败时整体掩码。
func sanitizeCDPEndpoint(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "(无法解析的 CDP 端点，已掩码)"
	}
	return parsed.Scheme + "://" + parsed.Host
}

// connectBoundedDialer 给建立的 conn 施加截止时间，使 websocket 握手读也受
// 超时约束（rod 的握手读不感知 ctx，无截止时间会无限阻塞）；握手成功后经
// release 解除截止时间，连接转入长期服役。支持 ws（明文）与 wss（TLS）。
type connectBoundedDialer struct {
	tls     bool          // wss 端点
	timeout time.Duration // 同时约束 dial 与握手读

	mu   sync.Mutex
	conn net.Conn
}

// normalizeWSSAddress 补齐 wss 缺显式端口的 ":443" 默认化：rod 的 initDialer
// 在存在自定义 Dialer 时直接返回、跳过了它对 wss 无端口补 ":443" 的语义，
// 这里在 dial 前补齐（明文 ws 不经此函数，维持 rod 原行为不做默认化）。
func normalizeWSSAddress(address string) string {
	if _, _, err := net.SplitHostPort(address); err != nil {
		// url.URL.Host 的 IPv6 形态自带方括号，JoinHostPort 会再加一层，先剥掉。
		return net.JoinHostPort(strings.Trim(address, "[]"), "443")
	}
	return address
}

func (d *connectBoundedDialer) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	if d.tls {
		address = normalizeWSSAddress(address)
	}
	dialer := &net.Dialer{Timeout: d.timeout}
	var conn net.Conn
	var err error
	if d.tls {
		// 与 rod 内置 tlsDialer 同为系统默认 TLS 配置。
		conn, err = (&tls.Dialer{NetDialer: dialer}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	// 截止时间同时约束握手读写（阻塞中的读会被截止时间打断）；不约束 dial
	// 后的长期使用——release 在握手成功后解除。
	_ = conn.SetDeadline(time.Now().Add(d.timeout))
	d.mu.Lock()
	d.conn = conn
	d.mu.Unlock()
	return conn, nil
}

// release 解除 conn 截止时间；websocket 握手成功后必须调用一次。
func (d *connectBoundedDialer) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		_ = d.conn.SetDeadline(time.Time{})
	}
}

// discard 关闭并清空持有的 conn：rod 的 WebSocket.Connect 在 dial 成功后
// 握手失败不会关闭已建立的连接，调用方必须在错误路径显式丢弃，否则 token
// 配错等握手失败场景会随 Job 重试逐次泄漏 fd。
func (d *connectBoundedDialer) discard() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
	}
}

// newRodNavigator 连接 CDP 端点（不回退本地启动浏览器，本项目不内嵌任何
// 浏览器二进制，spec §1）。
//
// 连接建立阶段受调用方 ctx 与 timeout 双重约束（dial 感知 ctx、握手读靠
// conn 截止时间），黑洞式端点不再长时间占用渲染槽；连接成功后浏览器切换到
// 连接生命周期 ctx——cdp.Client 的读写循环不依赖建连 ctx（按调用传 ctx），
// 建立的 net.Conn 也不绑定 dial ctx，切换不影响既有连接。
// 返回具体 *rodNavigator（TestCDPEndpoint 握手需访问 browser 客户端做
// Browser.getVersion）；调用方经 pageNavigator 接口消费，签名兼容 dial seam。
func newRodNavigator(ctx context.Context, cdpURL string, timeout time.Duration) (*rodNavigator, error) {
	parsed, err := url.Parse(strings.TrimSpace(cdpURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// go-rod/标准库的解析错误原样携带 URL（可能含 token 等敏感 query）：
		// 不再下传，只报脱敏端点。
		return nil, fmt.Errorf("CDP 端点不可达 (%s): 无法解析 CDP 端点", sanitizeCDPEndpoint(cdpURL))
	}
	connectTimeout := timeout
	if connectTimeout <= 0 {
		connectTimeout = defaultRenderTimeout
	}
	connectCtx, cancelConnect := context.WithTimeout(ctx, connectTimeout)
	defer cancelConnect()

	dialer := &connectBoundedDialer{tls: strings.EqualFold(parsed.Scheme, "wss"), timeout: connectTimeout}
	ws := &cdp.WebSocket{Dialer: dialer}
	if err := ws.Connect(connectCtx, cdpURL, nil); err != nil {
		// 握手失败时 rod 不关闭已建立的 conn：显式丢弃，防止反复重试泄漏 fd。
		dialer.discard()
		return nil, fmt.Errorf("CDP 端点不可达 (%s): %w", sanitizeCDPEndpoint(cdpURL), err)
	}
	dialer.release()

	lifecycleCtx, cancel := context.WithCancel(context.Background())
	browser := rod.New().Context(lifecycleCtx).Client(cdp.New().Start(ws))
	if err := browser.Connect(); err != nil {
		cancel()
		_ = ws.Close()
		return nil, fmt.Errorf("CDP 端点不可达 (%s): %w", sanitizeCDPEndpoint(cdpURL), err)
	}
	return &rodNavigator{
		ctx:          lifecycleCtx,
		cancel:       cancel,
		browser:      browser,
		ws:           ws,
		stableWindow: defaultRenderStableWindow,
	}, nil
}

// Navigate 渲染单页：打开新 target → UA 覆盖 → navigate → 等待渲染稳定
// （readyState + DOM 静默采样）→ outerHTML 与最终地址。
func (n *rodNavigator) Navigate(ctx context.Context, pageURL string) (renderResult, error) {
	page, err := n.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return renderResult{}, fmt.Errorf("打开渲染页失败: %w", err)
	}
	// 页面关闭绑定连接生命周期 ctx：抓取 ctx 超时/取消时 target 仍要被关闭，
	// 避免渲染端页面泄漏。
	defer func() { _ = page.Close() }()

	operating := page.Context(ctx)
	if n.userAgent != "" {
		if err := operating.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: n.userAgent}); err != nil {
			return renderResult{}, fmt.Errorf("设置渲染 UA 失败: %w", err)
		}
	}
	if err := operating.Navigate(pageURL); err != nil {
		return renderResult{}, fmt.Errorf("导航到 %s 失败: %w", pageURL, err)
	}
	if err := waitRenderStable(operating, n.stableWindow); err != nil {
		return renderResult{}, fmt.Errorf("等待页面渲染稳定失败: %w", err)
	}
	html, err := evalString(operating, "document.documentElement.outerHTML")
	if err != nil {
		return renderResult{}, fmt.Errorf("读取页面 outerHTML 失败: %w", err)
	}
	info, err := operating.Info()
	if err != nil {
		return renderResult{}, fmt.Errorf("读取渲染后地址失败: %w", err)
	}
	return renderResult{FinalURL: info.URL, HTML: html}, nil
}

// evalString 在页面上执行表达式并取字符串结果。
//
// 不用 rod 的 Page.Eval/HTML 系方法：它们的 JS 桥依赖
// Runtime.callFunctionOn 的 remote object 引用传参（把 helper 函数逐个挂到
// 上下文对象上），Obscura 等轻量 CDP 实现对该特性支持不完整（引用参数被
// 解析为 null，报 "Cannot set properties of null"）。原生 RuntimeEvaluate +
// ReturnByValue 是兼容面最大的取值路径（2026-09-17 经 Obscura v0.2.2 实测）。
func evalString(page *rod.Page, expression string) (string, error) {
	res, err := proto.RuntimeEvaluate{
		Expression:    expression,
		ReturnByValue: true,
	}.Call(page)
	if err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil {
		return "", fmt.Errorf("页面脚本执行异常: %s", res.ExceptionDetails.Text)
	}
	return res.Result.Value.String(), nil
}

// waitRenderStable 等待页面渲染稳定，两个阶段预算解耦：
//
//  1. 加载阶段：轮询 document.readyState 至 complete——不设独立 deadline，
//     跟随抓取请求 ctx（生产为 request_timeout_seconds，30s 量级；真实公网
//     页面 load 中位数在秒级，压进静默窗口会把慢页面错杀，见常量注释）；
//  2. 稳定阶段：在静默窗口内两次采样 outerHTML 长度，不变即认为 DOM 稳定；
//     该阶段总预算为 window×4，超时报「未达到渲染稳定」。
//
// 与 rod WaitStable 的差异（兼容性动机见 evalString）：不追踪网络静默，
// 用 DOM 尺寸采样近似——对抓取场景足够（正文注入完成即不再变化）；
// 抓取 ctx 取消随时中断。
func waitRenderStable(page *rod.Page, window time.Duration) error {
	pageCtx := page.GetContext()
	for {
		state, err := evalString(page, "document.readyState")
		if err != nil {
			return err
		}
		if state == "complete" {
			break
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-pageCtx.Done():
			return fmt.Errorf("页面加载未完成（抓取超时/取消）: %w", pageCtx.Err())
		}
	}
	stableDeadline := time.Now().Add(window * 4)
	for time.Now().Before(stableDeadline) {
		first, err := evalString(page, "document.documentElement.outerHTML.length")
		if err != nil {
			return err
		}
		select {
		case <-time.After(window):
		case <-pageCtx.Done():
			return fmt.Errorf("等待渲染稳定被中断: %w", pageCtx.Err())
		}
		second, err := evalString(page, "document.documentElement.outerHTML.length")
		if err != nil {
			return err
		}
		if first == second {
			return nil
		}
	}
	return fmt.Errorf("页面在 %s 内未达到渲染稳定", window*4)
}

// Close 关闭浏览器连接并终止其生命周期 ctx。
func (n *rodNavigator) Close() error {
	err := n.browser.Close()
	_ = n.ws.Close()
	n.cancel()
	return err
}
