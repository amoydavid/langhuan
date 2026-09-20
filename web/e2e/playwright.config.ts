// 全栈 E2E 冒烟（web 内容源，v1.3.0）编排：
//
//   琅嬛后端（standalone：SQLite + 进程内队列）— webServer 拉起本地二进制，
//     SPA 由 -tags web_embed 构建的内嵌产物服务（见根 Makefile e2e-web）；
//   embedding 桩与静态站点桩由 global-setup 经 docker compose 拉起（同生命
//     周期、同探测/销毁，见 docker-compose.e2e.yaml）：embedding 桩发布到
//     宿主 127.0.0.1:47702，经平台级 Provider（TrustedClient）调用；站点桩
//     挂公网形网桥地址 93.184.216.10:47701 作为抓取目标。
//
// 登录态：首个用例前经 UI 完成首管理员初始化 + 登录，落 storageState 复用。
// 全部端点均在本地。

import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, devices } from '@playwright/test'

const e2eDir = path.dirname(fileURLToPath(import.meta.url))
const tmpDir = path.join(e2eDir, '.tmp')
// 供 spec 读取同一临时目录（storageState/seed 文件路径与 cwd 无关）。
process.env.LANGHUAN_E2E_TMPDIR = tmpDir

// 全链路直连：本机常见系统代理（HTTP_PROXY/HTTPS_PROXY 等）会让 webServer 就绪
// 探测拿到代理的 502 响应而误判后端已启动，浏览器侧也可能经代理访问 127.0.0.1。
// E2E 的全部端点都在本机，这里统一剥离代理环境变量，并在浏览器启动参数里禁用代理。
for (const key of Object.keys(process.env)) {
  if (/^https?_proxy$|^all_proxy$|^no_proxy$/i.test(key)) {
    delete process.env[key]
  }
}

export default defineConfig({
  testDir: e2eDir,
  testMatch: ['web-source.spec.ts'],
  // 一条串行用户流程，单 worker 保证顺序与资源独占。
  fullyParallel: false,
  workers: 1,
  globalSetup: path.join(e2eDir, 'global-setup.ts'),
  globalTeardown: path.join(e2eDir, 'global-teardown.ts'),
  timeout: 120_000,
  expect: { timeout: 15_000 },
  retries: process.env.CI ? 1 : 0,
  outputDir: path.join(tmpDir, 'artifacts'),
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:18080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    locale: 'zh-CN',
    launchOptions: { args: ['--no-proxy-server'] },
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      // prepare-backend.mjs 重建数据目录并生成 config/credential.key，随后启动二进制。
      // reuseExistingServer 强制 false：复用旧后端会跳过 prepare-backend（数据目录
      // 不再全新），Makefile 已负责清理 18080 残留。
      command: `node ${path.join(e2eDir, 'support', 'prepare-backend.mjs')} && ${path.join(tmpDir, 'langhuan')} -config ${path.join(tmpDir, 'config.yaml')}`,
      url: 'http://127.0.0.1:18080/api/v1/healthz',
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: 'pipe',
      stderr: 'pipe',
    },
  ],
})
