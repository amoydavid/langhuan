// E2E 全局清理：销毁爬取目标编排（容器/网络），一次运行即弃。

import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const e2eDir = path.dirname(fileURLToPath(import.meta.url))
const composeFile = path.join(e2eDir, 'docker-compose.e2e.yaml')

export async function globalTeardown() {
  try {
    execFileSync(
      'docker',
      [
        'compose',
        '-f',
        composeFile,
        'down',
        '--remove-orphans',
        '--timeout',
        '5',
      ],
      { cwd: e2eDir, stdio: 'inherit' }
    )
  } catch (error) {
    console.warn(
      `[e2e] 清理 e2e 编排失败（可由 make e2e-web 兜底清理）: ${error}`
    )
  }
}

export default globalTeardown
