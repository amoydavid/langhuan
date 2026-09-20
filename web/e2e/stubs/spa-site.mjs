// render 引擎验证专用「JS-shell 站点」：正文完全由客户端 JS 渲染填充。
// http 引擎抓到的 HTML 只有空挂载点（isLikelyJSShell 命中）；只有真浏览器
// （Obscura/CDP render 引擎）执行脚本后才能拿到正文——这是 render 通道
// 与 http 通道的可区分性验证载体。
//
// 页面（3 页互链，seed=/）：
//   /                首页：#app 空挂载点 + JS 填充产品介绍与导航
//   /docs/rendering  文档页：JS 填充「渲染管线说明」正文
//   /docs/faq        FAQ 页：JS 填充两条问答
//
// 每页正文文本只在 <script> 字符串里出现（DOM 初始不含），带足够中文正文。

import http from 'node:http'

const PORT = 47801

const page = (title, bodyScript) => `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>${title} · 雾隐工坊</title>
</head>
<body>
<div id="app"></div>
<nav id="nav"></nav>
<script>
  const app = document.getElementById('app');
  ${bodyScript}
  const nav = document.getElementById('nav');
  nav.innerHTML = '<a href="/">首页</a> | <a href="/docs/rendering">渲染管线</a> | <a href="/docs/faq">常见问题</a>';
</script>
</body>
</html>`

const pages = {
  '/': page('雾隐工坊', `
    app.innerHTML = [
      '<h1>雾隐工坊 — 服务端渲染观察站</h1>',
      '<p>雾隐工坊是一个演示客户端渲染的示例站点：你看到的全部文字都由浏览器执行脚本后注入，原始 HTML 只有一个空挂载点。</p>',
      '<h2>我们做什么</h2>',
      '<p>收录各类无头浏览器对 JavaScript 页面的还原能力：文本、链接与结构是否与真实浏览器一致。纯静态抓取器在本站将一无所获。</p>'
    ].join('');`),
  '/docs/rendering': page('渲染管线说明', `
    app.innerHTML = [
      '<h1>渲染管线说明</h1>',
      '<p>浏览器先解析 HTML 构建 DOM，再执行脚本，脚本把字符串写入挂载点，最终布局与绘制发生在脚本执行之后。</p>',
      '<h2>对抓取器意味着什么</h2>',
      '<p>不执行 JavaScript 的抓取器只能看到空的挂载点；执行 JavaScript 的引擎能拿到与用户所见一致的正文。</p>'
    ].join('');`),
  '/docs/faq': page('常见问题', `
    app.innerHTML = [
      '<h1>常见问题</h1>',
      '<h2>为什么页面源码里没有正文？</h2>',
      '<p>正文由脚本注入，查看源代码只能看到空挂载点，这是客户端渲染站点的常态。</p>',
      '<h2>如何验证抓取器执行了脚本？</h2>',
      '<p>若抓取结果包含本段文字，说明脚本确实被执行，页面被完整渲染。</p>'
    ].join('');`),
  '/docs/slow-load': page('慢加载验证', `
    app.innerHTML = [
      '<h1>慢加载验证页</h1>',
      '<p>本页服务端延迟四秒响应，用于验证渲染引擎的加载阶段预算：慢页面不应被静默窗口错杀。</p>'
    ].join('');`),
  '/docs/escalation': page('自动升级验证', `
    app.innerHTML = [
      '<h1>自动升级验证页</h1>',
      '<p>本页用于验证自动升级链路：静态抓取器只能看到空挂载点，触发升级判定后由渲染引擎执行脚本取得本段正文。</p>',
      '<h2>判定依据</h2>',
      '<p>可见文本低于阈值且存在空挂载点，即判定为疑似脚本壳页面，需要真实浏览器渲染。</p>'
    ].join('');`),
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`)
  if (url.pathname === '/healthz') {
    res.writeHead(200, { 'content-type': 'text/plain' })
    res.end('ok')
    return
  }
  if (url.pathname === '/docs/slow-load') {
    // 服务端延迟 4s：render 引擎的加载阶段预算若仍压在静默窗口（2s）内，
    // 本页会被错杀为渲染失败——修复后应正常拿到 JS 注入正文。
    setTimeout(() => {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' })
      res.end(pages[url.pathname])
    }, 4000)
    return
  }
  const body = pages[url.pathname.replace(/\/$/, '') || '/']
  if (!body) {
    res.writeHead(404, { 'content-type': 'text/plain' })
    res.end('not found')
    return
  }
  res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' })
  res.end(body)
})

server.listen(PORT, () => {
  console.log(`[spa-site] listening on :${PORT}`)
})
