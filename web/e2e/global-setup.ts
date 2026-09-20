// E2E 全局准备：拉起全部外部依赖桩（爬取目标站点桩 + embedding 桩，见
// docker-compose.e2e.yaml）并做前置探测。探测是安全闸门：
//   - 站点桩：宿主进程必须能直达桩的公网形网桥地址（网桥直连路由）；不可达
//     说明宿主没有到该子网的路由（如 Docker Desktop），此时显式失败并销毁
//     编排——绝不带病启动后端，避免请求被路由到真实公网（探测包本身除外）。
//   - embedding 桩：宿主 127.0.0.1:47702 必须可达，否则索引链路必挂。

import { execFileSync } from 'node:child_process'
import { createServer } from 'node:net'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const SITE_BASE_URL = 'http://93.184.216.10:47701'
export const EMBEDDINGS_BASE_URL = 'http://127.0.0.1:47702'

const e2eDir = path.dirname(fileURLToPath(import.meta.url))
const composeFile = path.join(e2eDir, 'docker-compose.e2e.yaml')

function compose(args: string[]) {
  execFileSync('docker', ['compose', '-f', composeFile, ...args], {
    cwd: e2eDir,
    stdio: 'inherit',
  })
}

/** 探测 HTTP 端点就绪；deadline 内未就绪返回 false。 */
async function probeReady(url: string, deadline: number): Promise<boolean> {
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${url}/healthz`, {
        signal: AbortSignal.timeout(3_000),
      })
      if (response.ok) return true
    } catch {
      // 未就绪，继续重试直到超时。
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  return false
}

function teardown() {
  compose(['down', '--remove-orphans', '--timeout', '5'])
}

/** 端口占用 fail-fast：compose 发布端口前必须无人监听，否则绑定失败或误连旧进程。 */
function assertPortFree(port: number, hint: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const prober = createServer()
    prober.once('error', (error: NodeJS.ErrnoException) => {
      if (error.code === 'EADDRINUSE') {
        reject(
          new Error(
            `[e2e] 前置检查失败：127.0.0.1:${port} 已被占用（${hint}）。` +
              '请先清理残留进程（make e2e-web 会自动清理；直跑 pnpm e2e 需手动处理）再重试。'
          )
        )
        return
      }
      reject(error)
    })
    prober.listen(port, '127.0.0.1', () => {
      prober.close(() => resolve())
    })
  })
}

export async function globalSetup() {
  // 兜底清理同名项目（上次异常退出可能遗留），释放其发布端口。
  try {
    teardown()
  } catch {
    // 项目不存在时 down 会非零退出，忽略。
  }

  // 端口占用 fail-fast（必须在 compose up 之前：发布端口冲突只会报模糊错误，
  // 而残留监听进程会让被测后端误连旧桩，产出假绿）。
  await assertPortFree(47702, '残留的 embedding 桩进程或其它服务')

  compose(['up', '-d', '--wait'])

  const deadline = Date.now() + 15_000
  if (!(await probeReady(SITE_BASE_URL, deadline))) {
    teardown()
    throw new Error(
      `[e2e] 前置探测失败：宿主无法直达站点桩 ${SITE_BASE_URL}。` +
        '该地址位于 e2e 专用 docker 网桥的公网形子网（防 SSRF 误路由的安全闸门）；' +
        '宿主需要能直达网桥地址（OrbStack 默认支持；Docker Desktop 不支持）。' +
        '已销毁 e2e 编排，后端未启动。'
    )
  }
  if (!(await probeReady(EMBEDDINGS_BASE_URL, deadline))) {
    teardown()
    throw new Error(
      `[e2e] 前置探测失败：embedding 桩 ${EMBEDDINGS_BASE_URL} 不可达。已销毁 e2e 编排，后端未启动。`
    )
  }
  console.log(
    `[e2e] stubs ready: site=${SITE_BASE_URL} embeddings=${EMBEDDINGS_BASE_URL}`
  )
}

export default globalSetup
