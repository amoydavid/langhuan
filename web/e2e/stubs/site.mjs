// E2E 静态站点桩：程序化生成一个真实感的小型中文产品站，供琅嬛 web 内容源
// crawl 模式抓取。仅在测试编排下运行，不对外网发起或接受任何真实流量。
//
// 运行位置：web/e2e/docker-compose.e2e.yaml 里的 site 服务（node:20-alpine）。
// 之所以放容器里：被测后端的抓取链路带 SSRF 防护（spec §6.4），只允许
// 「公网单播」目标；本桩挂在 e2e 专用 docker 网络的公网形子网固定 IP 上，
// 该子网是 docker 网桥的直连路由，流量不离开宿主 VM（编排见 compose 文件注释）。
//
// 站点结构（5 页，互链，正文足够 readability 抽取）：
//   /                     星澜笔记主页（导航到其余 4 页）
//   /help/getting-started 快速开始
//   /help/faq             常见问题
//   /pricing/             定价
//   /blog/release-notes   更新日志
// 不提供 robots.txt（404）：按「允许抓取 + warning」语义走通 respect_robots=true 默认值。

import { createServer } from 'node:http'

const PORT = Number(process.env.SITE_PORT ?? 47701)

const layout = (title, body, active) => `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title} · 星澜笔记</title>
</head>
<body>
<header>
  <h1 class="brand"><a href="/">星澜笔记</a></h1>
  <nav>
    <a href="/"${active === '/' ? ' aria-current="page"' : ''}>首页</a>
    <a href="/help/getting-started"${active === '/help/getting-started' ? ' aria-current="page"' : ''}>快速开始</a>
    <a href="/help/faq"${active === '/help/faq' ? ' aria-current="page"' : ''}>常见问题</a>
    <a href="/pricing/"${active === '/pricing/' ? ' aria-current="page"' : ''}>定价</a>
    <a href="/blog/release-notes"${active === '/blog/release-notes' ? ' aria-current="page"' : ''}>更新日志</a>
  </nav>
</header>
<main>
${body}
</main>
<footer>
  <p>星澜笔记 · 一款面向个人知识管理的轻量笔记应用。</p>
  <p>© 2026 星澜笔记团队 · <a href="/help/faq">服务条款</a></p>
</footer>
</body>
</html>
`

const home = layout(
  '星澜笔记 — 把灵感装进一只笔记本',
  `<section>
  <h2>把灵感装进一只笔记本</h2>
  <p>星澜笔记是一款面向个人知识管理的轻量笔记应用，支持离线写作、全文检索与跨设备同步。</p>
  <p>我们相信笔记应当像星光一样：随手记录，随时点亮。安装后无需注册即可开始使用，所有数据默认保存在本机。</p>
</section>
<section>
  <h3>为什么选择星澜笔记</h3>
  <p>灵感收集箱让你把碎片想法先收进来，再慢慢整理；双向链接把相关笔记自然地连成一张知识网。</p>
  <p>全文检索基于本地索引，断网也能秒搜全部笔记；导出格式开放，数据永远属于你自己。</p>
  <p>想动手试试？请阅读<a href="/help/getting-started">快速开始</a>，或在<a href="/help/faq">常见问题</a>里查找答案。</p>
</section>`,
  '/'
)

const gettingStarted = layout(
  '快速开始',
  `<article>
  <h2>快速开始</h2>
  <p>本指南带你在五分钟内完成星澜笔记的安装与第一次写作。</p>
  <h3>第一步：安装</h3>
  <p>从首页下载对应平台的安装包，拖入应用程序文件夹即可。星澜笔记支持 macOS、Windows 与 Linux 三大平台。</p>
  <h3>第二步：创建第一篇笔记</h3>
  <p>点击左上角的「新建笔记」，输入标题与正文。灵感收集箱会自动保存每一次修改，无需手动保存。</p>
  <h3>第三步：整理与检索</h3>
  <p>用标签与文件夹归类笔记；按下快捷键 Cmd+K 呼出全文检索，输入关键词即可定位任意片段。</p>
  <p>更多技巧请查阅<a href="/help/faq">常见问题</a>，或关注<a href="/blog/release-notes">更新日志</a>了解新功能。</p>
</article>`,
  '/help/getting-started'
)

const faq = layout(
  '常见问题',
  `<article>
  <h2>常见问题</h2>
  <h3>笔记会占用多少存储空间？</h3>
  <p>纯文本笔记平均每篇小于 10 KB；即使包含图片，一千篇笔记通常也不到 1 GB。</p>
  <h3>换电脑时如何迁移数据？</h3>
  <p>在设置中选择「导出全部笔记」，得到一个压缩包；在新设备上选择「导入」即可完整还原。</p>
  <h3>两台设备同时修改同一篇笔记会冲突吗？</h3>
  <p>星澜笔记内置同步冲突保护：检测到冲突时保留两个版本并提示你合并，绝不静默覆盖内容。</p>
  <h3>支持多人协作吗？</h3>
  <p>当前版本专注个人知识管理；共享笔记本功能已列入路线图，详见<a href="/blog/release-notes">更新日志</a>。</p>
</article>`,
  '/help/faq'
)

const pricing = layout(
  '定价',
  `<article>
  <h2>定价</h2>
  <p>星澜笔记提供免费版与专业版两档，全部功能均可离线使用。</p>
  <h3>免费版</h3>
  <p>永久免费，不限笔记数量；包含灵感收集箱、全文检索与本地备份。适合刚开始整理知识的个人用户。</p>
  <h3>专业版</h3>
  <p>每月 12 元或每年 98 元：在免费版基础上增加跨设备同步、版本历史与主题定制。学生与教育工作者半价。</p>
  <h3>退款政策</h3>
  <p>专业版支持 30 天无理由退款；如对定价有疑问，请先浏览<a href="/help/faq">常见问题</a>。</p>
</article>`,
  '/pricing/'
)

const releaseNotes = layout(
  '更新日志',
  `<article>
  <h2>更新日志</h2>
  <h3>v1.3.0</h3>
  <p>新增灵感收集箱的快捷捕捉入口；全文检索现在支持拼音首字母；修复深色模式下代码块对比度过低的问题。</p>
  <h3>v1.2.0</h3>
  <p>深色模式全面上线，跟随系统外观自动切换；优化启动速度 40%；笔记导出新增 EPUB 格式。</p>
  <h3>v1.1.0</h3>
  <p>引入双向链接与反向面板；灵感收集箱支持拖拽排序；修复若干同步边界问题。</p>
  <p>下一个版本将带来共享笔记本的公开测试，敬请期待。</p>
</article>`,
  '/blog/release-notes'
)

const routes = {
  '/': { status: 200, body: home },
  '/help/getting-started': { status: 200, body: gettingStarted },
  '/help/faq': { status: 200, body: faq },
  '/pricing/': { status: 200, body: pricing },
  '/blog/release-notes': { status: 200, body: releaseNotes },
}

const server = createServer((request, response) => {
  const path = new URL(request.url, `http://${request.headers.host ?? 'localhost'}`)
    .pathname
  console.log(`[site] ${request.method} ${path}`)

  if (path === '/healthz') {
    response.writeHead(200, { 'content-type': 'text/plain; charset=utf-8' })
    response.end('ok')
    return
  }

  const route = routes[path]
  if (!route) {
    response.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' })
    response.end('not found')
    return
  }
  response.writeHead(route.status, { 'content-type': 'text/html; charset=utf-8' })
  response.end(route.body)
})

server.listen(PORT, '0.0.0.0', () => {
  console.log(`[site] e2e static site listening on 0.0.0.0:${PORT}`)
})
