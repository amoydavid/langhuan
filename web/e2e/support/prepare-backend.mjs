// E2E 后端编排准备脚本：为 standalone（SQLite / 无 Redis）模式的琅嬛二进制
// 生成一次性运行环境，保证每次 Playwright 运行都是全新数据：
//   1. 清空并重建 web/e2e/.tmp/data（SQLite 库、原始文档目录随之一新）；
//   2. 生成一次性 credential.key（Base64 32 字节，凭证主密钥）；
//   3. 生成 web/e2e/.tmp/config.yaml（监听 127.0.0.1:18080，绝不含真实凭证）。
// 由 playwright.config.ts 的 webServer.command 在每次运行前调用，幂等。

import { randomBytes } from 'node:crypto'
import { mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const e2eDir = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const tmpDir = path.join(e2eDir, '.tmp')
const dataDir = path.join(tmpDir, 'data')
const keyPath = path.join(tmpDir, 'credential.key')
const configPath = path.join(tmpDir, 'config.yaml')

// 每次运行全新数据目录：SQLite 库、会话、原始文档一律不留持久状态。
rmSync(dataDir, { recursive: true, force: true })
mkdirSync(path.join(dataDir, 'raw-documents'), { recursive: true })

writeFileSync(keyPath, randomBytes(32).toString('base64'), { mode: 0o600 })

const toSlash = (value) => value.split(path.sep).join('/')
const config = `# E2E standalone 配置（由 support/prepare-backend.mjs 生成，勿手编）。
server:
  http_addr: 127.0.0.1:18080
  base_url: http://127.0.0.1:18080
  run_http: true
  run_worker: true
database:
  driver: sqlite
  dsn: file:${toSlash(path.join(dataDir, 'langhuan.db'))}?cache=shared
  auto_migrate: true
redis:
  enabled: false
storage:
  driver: local
  raw_document_dir: ${toSlash(path.join(dataDir, 'raw-documents'))}
auth:
  session:
    secure_cookie: false
  password:
    enabled: true
credentials:
  encryption_key_file: ${toSlash(keyPath)}
web_source:
  request_timeout_seconds: 15
  default_request_delay_ms: 10
`

writeFileSync(configPath, config)
console.log(`[e2e] backend ready: ${configPath}`)
