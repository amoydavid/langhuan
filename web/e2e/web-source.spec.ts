// 全栈 E2E 冒烟：web 内容源（v1.3.0）用户流程。
//
// 真实后端进程（standalone：SQLite + 进程内队列 + 内嵌 SPA 产物）+ 真实浏览器：
//   首管理员初始化与登录（UI） → API 种子（工作区/模型/知识库） →
//   Web 子 tab「添加网站源」（crawl，指向本地静态站点桩） → 等同步完成 →
//   内容 tab 看到抓取页面 → 检索 tab 查询命中。
//
// 全部端点均在本地：站点桩与 embedding 桩都由 global-setup 经 docker compose
// 拉起（站点桩挂 e2e 专用网桥的公网形地址；embedding 桩发布到宿主
// 127.0.0.1:47702，经平台级 Provider 调用），与被测后端同生命周期。

import fs from 'node:fs'
import path from 'node:path'
import {
  type APIRequestContext,
  expect,
  type Page,
  test,
} from '@playwright/test'

// playwright.config.ts 在加载期导出，测试进程内恒定可用。
const tmpDir = process.env.LANGHUAN_E2E_TMPDIR ?? path.resolve('e2e/.tmp')
const authStatePath = path.join(tmpDir, 'auth.json')
const seedStatePath = path.join(tmpDir, 'seed.json')

// 与 stubs/embeddings.mjs、docker-compose.e2e.yaml 对齐的本地端点。
const EMBEDDINGS_BASE_URL = 'http://127.0.0.1:47702/v1'
const SITE_SEED_URL = 'http://93.184.216.10:47701/'

const ADMIN_EMAIL = 'admin@langhuan-e2e.local'
const ADMIN_NICKNAME = '琅嬛管理员'
const ADMIN_PASSWORD = 'e2e-Passw0rd'

const WORKSPACE = { name: '琅嬛 E2E', slug: 'e2e-web-source' }
const KB_NAME = '星澜笔记知识库'
const SOURCE_NAME = '星澜笔记文档站'
// 静态站点共 5 页（见 stubs/site.mjs）；同步成功的判据是全部页面入库且零失败。
const SITE_PAGE_COUNT = 5

test.describe.configure({ mode: 'serial' })
test.use({ storageState: authStatePath })

type SeedState = {
  workspaceSlug: string
  knowledgeBaseId: string
}

let seed: SeedState

function loadSeed(): SeedState {
  if (seed) return seed
  seed = JSON.parse(fs.readFileSync(seedStatePath, 'utf8')) as SeedState
  return seed
}

/** 页面上下文同源的 API 客户端：自动携带登录会话 cookie。 */
function api(request: APIRequestContext) {
  return {
    async post(
      pathname: string,
      body: unknown
    ): Promise<Record<string, unknown>> {
      const response = await request.post(pathname, { data: body })
      const text = await response.text()
      if (!(response.status() === 200 || response.status() === 201)) {
        throw new Error(`POST ${pathname} -> ${response.status()}: ${text}`)
      }
      return text ? (JSON.parse(text) as Record<string, unknown>) : {}
    },
    async get(pathname: string): Promise<{ status: number; json: unknown }> {
      const response = await request.get(pathname)
      const text = await response.text()
      return { status: response.status(), json: text ? JSON.parse(text) : null }
    },
  }
}

async function bootstrapAndLoginAsAdmin(browserPage: Page): Promise<void> {
  await browserPage.goto('/')
  // 全新数据目录：未初始化时根路径重定向到 /setup（首管理员引导）。
  await browserPage.getByLabel('邮箱').waitFor({ timeout: 30_000 })

  if (browserPage.url().includes('/setup')) {
    await browserPage.getByLabel('邮箱').fill(ADMIN_EMAIL)
    await browserPage.getByLabel('昵称').fill(ADMIN_NICKNAME)
    await browserPage.getByLabel('密码', { exact: true }).fill(ADMIN_PASSWORD)
    await browserPage.getByLabel('确认密码').fill(ADMIN_PASSWORD)
    await browserPage.getByRole('button', { name: '完成初始化' }).click()
    // 初始化成功后跳转登录页。
    await browserPage.waitForURL(/\/sign-in/)
  }

  await browserPage.getByLabel('邮箱').fill(ADMIN_EMAIL)
  await browserPage.getByLabel('密码', { exact: true }).fill(ADMIN_PASSWORD)
  await browserPage.getByRole('button', { name: '登录', exact: true }).click()
  // 登录成功后离开 /sign-in（进入工作区入口页）。
  await browserPage.waitForURL((url) => !url.pathname.includes('/sign-in'), {
    timeout: 30_000,
  })
}

test.beforeAll(async ({ browser }) => {
  // 每次运行后端都是全新数据目录（见 support/prepare-backend.mjs），
  // 因此固定重走「初始化管理员 → 登录」UI 流程并覆写 storageState。
  // 显式空 storageState 覆盖 test.use 的文件路径（此时文件尚未生成）。
  const context = await browser.newContext({
    locale: 'zh-CN',
    storageState: { cookies: [], origins: [] },
  })
  const browserPage = await context.newPage()
  await bootstrapAndLoginAsAdmin(browserPage)
  fs.mkdirSync(tmpDir, { recursive: true })
  fs.writeFileSync(authStatePath, JSON.stringify(await context.storageState()))
  await context.close()
})

test('种子数据：创建工作区、平台级 embedding Provider/模型与知识库', async ({
  request,
}) => {
  const client = api(request)

  await client.post('/api/v1/workspaces', WORKSPACE)

  // embedding 桩挂在回环地址：只有平台级 Provider 走 TrustedClient（允许明文 HTTP
  // 与私有地址）；workspace 级 Provider 会命中 SSRF 公网校验，因此这里用平台级。
  const provider = await client.post('/api/v1/admin/model-providers', {
    name: 'e2e-openai-stub',
    display_name: 'E2E OpenAI 兼容桩',
    description: '仅用于 e2e 冒烟的本地 embedding 桩',
    provider: 'openai',
    config: {
      mode: 'standard',
      base_url: EMBEDDINGS_BASE_URL,
      timeout_seconds: 60,
    },
    credentials: { api_key: 'e2e-local-stub-key' },
  })
  const model = await client.post(
    `/api/v1/admin/model-providers/${provider.id}/models`,
    {
      name: 'e2e-embedding',
      display_name: 'E2E Embedding',
      description: '确定性向量桩，维度 1024',
      type: 'embedding',
      model_name: 'stub-embedding',
      dimensions: 1024,
    }
  )
  const knowledgeBase = await client.post(
    `/api/v1/workspaces/${WORKSPACE.slug}/knowledge-bases`,
    {
      name: KB_NAME,
      description: 'e2e web 内容源冒烟用知识库',
      embedding_model_id: model.id,
    }
  )

  const webSources = await client.get(
    `/api/v1/workspaces/${WORKSPACE.slug}/knowledge-bases/${knowledgeBase.id}/web-sources`
  )
  expect(webSources.status).toBe(200)
  expect(webSources.json).toEqual([])

  seed = {
    workspaceSlug: WORKSPACE.slug,
    knowledgeBaseId: String(knowledgeBase.id),
  }
  fs.writeFileSync(seedStatePath, JSON.stringify(seed))
})

test('Web 子 tab：添加 crawl 模式网站源（本地静态站点）', async ({ page }) => {
  const { workspaceSlug, knowledgeBaseId } = loadSeed()
  // 首个源创建前 Web 子 tab 不出现在内容导航（spec §9.4），按深链直达。
  await page.goto(
    `/workspaces/${workspaceSlug}/kb/${knowledgeBaseId}/content/web`
  )

  await page.getByRole('button', { name: '添加网站源' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()

  // 默认即 crawl 模式；断言单选组第一项（爬取）处于选中态。
  const modeGroup = dialog.getByRole('radiogroup')
  await expect(modeGroup.getByText('爬取', { exact: true })).toBeVisible()
  await expect(modeGroup.getByRole('radio').first()).toBeChecked()

  await dialog.getByLabel('名称').fill(SOURCE_NAME)
  await dialog.getByLabel('种子 URL').fill(SITE_SEED_URL)

  // 高级选项保持默认：respect_robots 默认开启（站点桩无 robots.txt → 允许 + warning）。
  await dialog.getByRole('button', { name: '高级选项' }).click()
  await expect(dialog.getByLabel('遵守 robots.txt')).toBeChecked()

  await dialog.getByRole('button', { name: '添加并入队' }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByText(SOURCE_NAME).first()).toBeVisible()
})

test('同步完成：源页面计数为 5 且零失败', async ({ page, request }) => {
  const { workspaceSlug, knowledgeBaseId } = loadSeed()
  const webSourcesPath = `/api/v1/workspaces/${workspaceSlug}/knowledge-bases/${knowledgeBaseId}/web-sources`

  // 轮询 web-sources API 直到全部页面入库且无失败页（超时 90s）。
  await expect
    .poll(
      async () => {
        const response = await request.get(webSourcesPath)
        if (!response.ok()) return null
        const views = (await response.json()) as Array<{
          page_count: number
          failed_page_count: number
        }>
        return views[0] ?? null
      },
      { timeout: 90_000, intervals: [1_000] }
    )
    .toEqual(
      expect.objectContaining({
        page_count: SITE_PAGE_COUNT,
        failed_page_count: 0,
      })
    )

  // 刷新 Web tab：源卡片应展示非 0 页面计数。
  await page.goto(
    `/workspaces/${workspaceSlug}/kb/${knowledgeBaseId}/content/web`
  )
  const card = page.getByText(SOURCE_NAME).first()
  await expect(card).toBeVisible()
  await expect(page.getByText(`页面 ${SITE_PAGE_COUNT}`)).toBeVisible()
})

test('内容 tab：抓取到的页面进入知识库并就绪', async ({ page, request }) => {
  const { workspaceSlug, knowledgeBaseId } = loadSeed()
  const documentsPath = `/api/v1/workspaces/${workspaceSlug}/knowledge-bases/${knowledgeBaseId}/documents`

  // 等待解析/分块/embedding 全部完成（文档状态 ready），再驱动 UI 断言。
  await expect
    .poll(
      async () => {
        const response = await request.get(documentsPath)
        if (!response.ok()) return null
        const documents = (await response.json()) as Array<{
          kind: string
          status: string
        }>
        if (documents.length !== SITE_PAGE_COUNT) return null
        return documents.every(
          (document) => document.kind === 'web' && document.status === 'ready'
        )
      },
      { timeout: 90_000, intervals: [1_000] }
    )
    .toBe(true)

  await page.goto(
    `/workspaces/${workspaceSlug}/kb/${knowledgeBaseId}/content/all`
  )
  await expect(page.getByText('快速开始').first()).toBeVisible()
  await expect(page.getByText('常见问题').first()).toBeVisible()
  await expect(page.getByText('更新日志').first()).toBeVisible()
})

test('检索 tab：查询命中抓取的 web 页面', async ({ page }) => {
  const { workspaceSlug, knowledgeBaseId } = loadSeed()
  await page.goto(`/workspaces/${workspaceSlug}/kb/${knowledgeBaseId}/search`)

  // 「深色模式」只出现在站点更新日志页正文，验证检索链路命中 web 文档。
  await page.getByLabel('检索问题').fill('深色模式')
  await page.getByRole('button', { name: '检索', exact: true }).click()

  await expect(page.getByText('检索证据')).toBeVisible()
  const webBadge = page.getByText('web', { exact: true }).first()
  await expect(webBadge).toBeVisible()
  // 命中文档应是更新日志页（标题由 readability 抽取，可能带站点后缀）。
  await expect(page.getByText(/更新日志/).first()).toBeVisible()
})
