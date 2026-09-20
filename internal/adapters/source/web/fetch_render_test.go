package web

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// render 引擎单测：真实 CDP 需要浏览器，成功路径/私网拒绝/并发上限经注入 fake
// 覆盖；CDP 不可达用 newRodNavigator 指向无监听端点断言连接失败。真实渲染不过度
// 测试（集成阶段人工验收）。

// fakeNavigator 是 pageNavigator 的可编程 fake。
type fakeNavigator struct {
	mu     sync.Mutex
	urls   []string
	result renderResult
	err    error
	// gate 非空时：进入 Navigate 后等待信号再返回（并发上限测试用）。
	gate   chan struct{}
	closed bool
}

func (n *fakeNavigator) Navigate(_ context.Context, pageURL string) (renderResult, error) {
	n.mu.Lock()
	n.urls = append(n.urls, pageURL)
	gate := n.gate
	n.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if n.err != nil {
		return renderResult{}, n.err
	}
	return n.result, nil
}

func (n *fakeNavigator) Close() error {
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
	return nil
}

func (n *fakeNavigator) navigatedURLs() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.urls...)
}

func (n *fakeNavigator) isClosed() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closed
}

func newTestRenderFetcher(t *testing.T, nav *fakeNavigator) (*RenderFetcher, *int) {
	t.Helper()
	dials := 0
	fetcher, err := NewRenderFetcher(RenderFetcherConfig{
		CDPURL:         "ws://cdp.test",
		validateTarget: passthroughValidate,
		dial: func(_ context.Context, cdpURL string, _ time.Duration) (pageNavigator, error) {
			dials++
			assert.Equal(t, "ws://cdp.test", cdpURL)
			return nav, nil
		},
	})
	require.NoError(t, err)
	return fetcher, &dials
}

func TestRenderCDPEndpoint(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ws://cdp.test", RenderCDPEndpoint("ws://cdp.test", ""))
	assert.Equal(t, "ws://cdp.test?token=abc", RenderCDPEndpoint("ws://cdp.test", "abc"))
	assert.Equal(t, "ws://cdp.test/devtools?a=1&token=a%2Fb", RenderCDPEndpoint("ws://cdp.test/devtools?a=1", "a/b"),
		"已有 query 时追加，token 做 query 转义")
	assert.Equal(t, "", RenderCDPEndpoint("  ", ""), "空端点原样为空（装配校验在构造器）")
}

func TestRenderFetchSuccessAndConnectionReuse(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{result: renderResult{
		FinalURL: "https://example.com/final",
		HTML:     `<html><body><p>rendered body</p><a href="/next">next</a><a href="">empty</a></body></html>`,
	}}
	fetcher, dials := newTestRenderFetcher(t, nav)

	for i := 0; i < 3; i++ {
		page, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/page"})
		require.NoError(t, err)
		assert.Equal(t, EngineRender, page.Engine)
		assert.Equal(t, 200, page.Status, "渲染结果映射等价状态 200")
		assert.Equal(t, "https://example.com/final", page.FinalURL)
		assert.Contains(t, string(page.HTML), "rendered body")
		assert.Equal(t, []string{"/next"}, page.Links, "出链取原始 href，空值丢弃")
		assert.Empty(t, page.Markdown, "render 通道不填 markdown（抽取层负责）")
		assert.Empty(t, page.ETag, "render 通道无条件请求凭据")
	}
	assert.Equal(t, 1, *dials, "浏览器连接跨页复用，只建立一次")
	require.Len(t, nav.navigatedURLs(), 3)
}

func TestRenderFetchFinalURLFallsBackToRequest(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{result: renderResult{HTML: "<html><body>x</body></html>"}}
	fetcher, _ := newTestRenderFetcher(t, nav)

	page, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/page"})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/page", page.FinalURL, "navigator 未给出最终地址时回退请求 URL")
}

func TestRenderFetchRejectsPrivateTargetBeforeNavigation(t *testing.T) {
	t.Parallel()

	// 真实校验器（validateTarget 未注入）：私网字面量在导航前拒绝，且不建连。
	nav := &fakeNavigator{result: renderResult{HTML: "<html><body>x</body></html>"}}
	fetcher, err := NewRenderFetcher(RenderFetcherConfig{
		CDPURL: "ws://cdp.test",
		dial: func(context.Context, string, time.Duration) (pageNavigator, error) {
			return nav, nil
		},
	})
	require.NoError(t, err)

	_, err = fetcher.Fetch(t.Context(), FetchRequest{URL: "http://127.0.0.1/private"})
	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeSkipped, pageErr.Outcome)
	assert.Empty(t, nav.navigatedURLs(), "私网目标不得发起导航")
	assert.False(t, nav.isClosed())
}

func TestRenderFetchOversize(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{result: renderResult{
		HTML: "<html><body>" + strings.Repeat("x", 128) + "</body></html>",
	}}
	fetcher, _ := newTestRenderFetcher(t, nav)

	_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/big", MaxContentBytes: 32})
	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeOversize, pageErr.Outcome)
	assert.ErrorIs(t, err, sourceport.ErrSourceContentTooLarge)
}

func TestRenderFetchNavigatorErrorIsRetryable(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{err: errors.New("navigation failed")}
	fetcher, _ := newTestRenderFetcher(t, nav)

	_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/page"})
	require.Error(t, err)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err), "渲染失败按 retryable 归类，Job 级重试兜底")
}

func TestRenderFetchRespectsGlobalSemaphore(t *testing.T) {
	t.Parallel()

	// 信号量上限 1：第二个抓取必须等第一个释放后才能进入 navigator。
	gate := make(chan struct{})
	nav := &fakeNavigator{result: renderResult{HTML: "<html><body>x</body></html>"}, gate: gate}
	fetcher, _ := newTestRenderFetcher(t, nav)
	fetcher.limiter = newRenderLimiter(1)

	const total = 3
	var wg sync.WaitGroup
	inFlight := make(chan struct{}, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/page"})
			assert.NoError(t, err)
			inFlight <- struct{}{}
		}()
	}
	// 首个进入 Navigate 后放行一次；由于信号量上限 1，任意时刻最多一个在导航，
	// 逐次放行即可全部完成。
	for i := 0; i < total; i++ {
		gate <- struct{}{}
	}
	wg.Wait()
	close(gate)
	assert.Len(t, nav.navigatedURLs(), total)
}

func TestRenderFetchContextCancelWhileWaitingForSlot(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{result: renderResult{HTML: "<html><body>x</body></html>"}}
	fetcher, _ := newTestRenderFetcher(t, nav)
	fetcher.limiter = newRenderLimiter(1)

	// 占住唯一的渲染槽。
	release, err := fetcher.limiter.acquire(t.Context())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = fetcher.Fetch(ctx, FetchRequest{URL: "https://example.com/page"})
	require.Error(t, err)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err), "排队被 ctx 中断按 retryable 归类")
	assert.Empty(t, nav.navigatedURLs())
}

func TestRenderFetcherClose(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{result: renderResult{HTML: "<html><body>x</body></html>"}}
	fetcher, dials := newTestRenderFetcher(t, nav)

	_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/page"})
	require.NoError(t, err)

	fetcher.Close()
	fetcher.Close() // 重复关闭安全
	assert.True(t, nav.isClosed(), "Close 应释放浏览器连接")

	_, err = fetcher.Fetch(t.Context(), FetchRequest{URL: "https://example.com/page"})
	require.Error(t, err)
	assert.Equal(t, 1, *dials, "关闭后不再重建连接")
}

func TestNewRenderFetcherRequiresCDPURL(t *testing.T) {
	t.Parallel()

	_, err := NewRenderFetcher(RenderFetcherConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint_url")
}

// TestRodNavigatorConnectFails 断言真实 rod 通道对不可达 CDP 的失败路径：
// 只做连接、不回退本地启动浏览器；错误只携带脱敏端点。
func TestRodNavigatorConnectFails(t *testing.T) {
	t.Parallel()

	nav, err := newRodNavigator(t.Context(), "http://127.0.0.1:1", time.Second)
	require.Error(t, err)
	assert.Nil(t, nav)
	assert.Contains(t, err.Error(), "CDP 端点不可达")
	assert.Contains(t, err.Error(), "http://127.0.0.1:1", "脱敏端点保留 scheme://host 便于诊断")
}

// TestRodNavigatorConnectPhaseBoundedByTimeout 断言连接阶段受 timeout 约束：
// 黑洞式端点（TCP 可达但不回应 websocket 握手）不得长时间挂住建连。
func TestRodNavigatorConnectPhaseBoundedByTimeout(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			// 接受连接后不读不写（模拟防火墙 drop / 握手无响应），
			// 仅设截止时间防止测试侧连接悬挂。
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		}
	}()

	start := time.Now()
	nav, err := newRodNavigator(t.Context(), "http://"+listener.Addr().String(), 300*time.Millisecond)
	require.Error(t, err)
	assert.Nil(t, nav)
	assert.Less(t, time.Since(start), 5*time.Second,
		"连接阶段（dial/握手）应被 timeout 约束，黑洞式端点不得长挂")
}

// TestConnectBoundedDialerWSSDefaultsMissingPort 断言 wss 缺显式端口时补
// ":443" 默认化：rod 的 initDialer 在自定义 Dialer 下跳过该默认化，若不补齐
// 则 wss://chrome.browserless.io 这类形态会以缺端口地址 dial 失败（spec 推荐
// 部署链）。明文 ws 维持 rod 原行为：缺端口不默认化。
func TestConnectBoundedDialerWSSDefaultsMissingPort(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:443")
	if err != nil {
		t.Skipf("绑定 127.0.0.1:443 需要权限（%v），跳过缺端口默认化断言", err)
	}
	defer listener.Close()

	accepted := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		accepted <- conn.RemoteAddr().String()
		_ = conn.Close()
	}()

	// 缺端口地址：应按 wss 语义默认化补 :443，连接到达本测试的监听端。
	dialer := &connectBoundedDialer{tls: true, timeout: 2 * time.Second}
	_, _ = dialer.DialContext(t.Context(), "tcp", "127.0.0.1")

	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("wss 缺端口地址未被默认化补 :443（rod initDialer 跳过语义未补齐）")
	}

	// 明文 ws 对照：缺端口不默认化，保持 rod 原行为直接报 missing port。
	plain := &connectBoundedDialer{timeout: time.Second}
	_, err = plain.DialContext(t.Context(), "tcp", "127.0.0.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing port")
}

// TestRodNavigatorWSSEndToEndDial 走真实 newRodNavigator 的 wss 通道：
// TLS listener（自签证书）上 dial 必须越过地址/端口层，失败不得是 missing port。
func TestRodNavigatorWSSEndToEndDial(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{selfSignedCertificate(t)},
	})
	defer tlsListener.Close()
	go func() {
		for {
			conn, acceptErr := tlsListener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			_ = conn.Close() // 客户端侧 TLS 校验必然失败（自签），无需真实握手应答
		}
	}()

	_, err = newRodNavigator(t.Context(), "wss://"+listener.Addr().String(), 2*time.Second)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "missing port", "wss 通道不得以缺端口地址进 dial")
}

// TestRodNavigatorClosesConnOnHandshakeFailure 断言握手失败（rod 不关已建立的
// conn）时错误路径关闭连接：否则 token 配错等场景随 Job 重试逐次泄漏 fd。
func TestRodNavigatorClosesConnOnHandshakeFailure(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	peerClosed := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		// 回以非 101 响应：websocket 握手快速失败（BadHandshakeError）。
		_, _ = conn.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
		// 排空读直至 EOF：客户端在途的升级请求字节会先到达，修复后错误路径
		// discard 关闭客户端 conn → 排空后必见 EOF；未修复（泄漏）时只能等
		// 对端超时，读永远等不到 EOF。
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if _, readErr := conn.Read(make([]byte, 512)); readErr != nil {
				close(peerClosed)
				return
			}
		}
	}()

	_, navErr := newRodNavigator(t.Context(), "ws://"+listener.Addr().String(), 2*time.Second)
	require.Error(t, navErr, "403 响应应使握手失败")

	select {
	case <-peerClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("握手失败后客户端必须关闭连接（rod 不代关），否则反复重试泄漏 fd")
	}
}

// TestNormalizeWSSAddress 单元覆盖 wss 缺端口默认化（rod initDialer 在自定义
// Dialer 下跳过的语义）；明文 ws 不调用该函数，维持 rod 原行为。
func TestNormalizeWSSAddress(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "chrome.browserless.io:443", normalizeWSSAddress("chrome.browserless.io"),
		"spec 推荐部署链的无端口 wss 形态必须默认化 :443")
	assert.Equal(t, "127.0.0.1:443", normalizeWSSAddress("127.0.0.1"))
	assert.Equal(t, "[::1]:443", normalizeWSSAddress("[::1]"), "IPv6 缺端口同样补齐")
	assert.Equal(t, "127.0.0.1:8443", normalizeWSSAddress("127.0.0.1:8443"), "显式端口不动")
	assert.Equal(t, "cdp.test:443", normalizeWSSAddress("cdp.test:443"), "已是 443 不重复补")
}

// selfSignedCertificate 生成仅用于测试监听端的自签证书。
func selfSignedCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "langhuan-render-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// TestRodNavigatorMasksEndpointInErrors 断言畸形端点与带 token 的端点在错误
// 消息中脱敏：token query 绝不进入错误链。
func TestRodNavigatorMasksEndpointInErrors(t *testing.T) {
	t.Parallel()

	t.Run("畸形端点整体掩码", func(t *testing.T) {
		t.Parallel()
		_, err := newRodNavigator(t.Context(), "ws://bad host:1/?token=sekrit", time.Second)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "sekrit", "token 不得进入错误消息")
		assert.NotContains(t, err.Error(), "bad host", "无法解析的原始 URL 不得进入错误消息")
		assert.Contains(t, err.Error(), "已掩码")
	})

	t.Run("合法端点的错误只报 scheme://host", func(t *testing.T) {
		t.Parallel()
		_, err := newRodNavigator(t.Context(), "http://127.0.0.1:1/?token=sekrit", time.Second)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "sekrit", "token 不得进入错误消息")
		assert.Contains(t, err.Error(), "http://127.0.0.1:1")
	})
}

func TestSanitizeCDPEndpoint(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ws://cdp.test:9222", sanitizeCDPEndpoint("ws://cdp.test:9222?token=abc"))
	assert.Equal(t, "http://127.0.0.1:9222", sanitizeCDPEndpoint("http://user:pass@127.0.0.1:9222/devtools?a=1"))
	assert.Equal(t, "(无法解析的 CDP 端点，已掩码)", sanitizeCDPEndpoint("ws://bad host/?token=x"))
	assert.Equal(t, "(无法解析的 CDP 端点，已掩码)", sanitizeCDPEndpoint("not a url"))
	assert.Equal(t, "(无法解析的 CDP 端点，已掩码)", sanitizeCDPEndpoint(""))
}

// TestRenderFetchConnectWaitHonorsContextCancellation 断言连接等待阶段可被
// 请求 ctx 取消（timeout 参数正确下发），取消后按 retryable 归类。
func TestRenderFetchConnectWaitHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	nav := &fakeNavigator{result: renderResult{HTML: "<html><body>x</body></html>"}}
	fetcher, _ := newTestRenderFetcher(t, nav)
	fetcher.timeout = 2 * time.Second

	started := make(chan struct{})
	fetcher.dial = func(dialCtx context.Context, cdpURL string, timeout time.Duration) (pageNavigator, error) {
		assert.Equal(t, 2*time.Second, timeout, "渲染超时应下发到连接阶段")
		close(started)
		<-dialCtx.Done() // 模拟黑洞式端点：dial 挂住直至 ctx 取消
		return nil, dialCtx.Err()
	}

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()
	_, err := fetcher.Fetch(ctx, FetchRequest{URL: "https://example.com/page"})
	require.Error(t, err)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err), "连接阶段被 ctx 中断按 retryable 归类")
	assert.Empty(t, nav.navigatedURLs())
}

// 编译期保证 fakeNavigator 满足接口。
var _ pageNavigator = (*fakeNavigator)(nil)
