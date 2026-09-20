// E2E embedding 桩：OpenAI 兼容的 POST /v1/embeddings，返回确定性向量
// （按输入文本 hash 派生，维度 1024，同文本恒同向量），供琅嬛的
// OpenAI-compatible embedding adapter 在本地完成全链路索引/检索。
// 容器内监听 0.0.0.0（docker-proxy 转发需要）；宿主侧仅发布 127.0.0.1:47702，不对外暴露。
//
// 向量派生方式（确定性，无语义）：对文本按滑动窗口取 FNV-1a 哈希填充固定维度，
// 再做 L2 归一化。评测指标无语义意义，只保证「同文本同向量、异文本异向量」，
// 足以驱动向量索引/RRF 融合链路。

import { createHash } from 'node:crypto'
import { createServer } from 'node:http'

// 容器内必须绑定 0.0.0.0（docker 端口发布经 docker-proxy 转发到容器 IP）；
// 宿主侧仅发布 127.0.0.1:47702，不对外暴露。
const HOST = process.env.EMBEDDINGS_HOST ?? '0.0.0.0'
const PORT = Number(process.env.EMBEDDINGS_PORT ?? 47702)
const DIMENSIONS = 1024

/** FNV-1a 32 位哈希。 */
function fnv1a(text, seed) {
  let hash = seed >>> 0
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193) >>> 0
  }
  return hash >>> 0
}

/** 由文本派生确定性单位向量（L2 归一化后的 1024 维）。 */
function embed(text) {
  // 用 sha256 派生 8 个相互独立的 FNV 种子，增强不同文本间的向量差异。
  const digest = createHash('sha256').update(text, 'utf8').digest()
  const vector = new Float64Array(DIMENSIONS)
  for (let start = 0; start < DIMENSIONS; start += 1) {
    const seed = digest[start % digest.length] * (start + 1)
    vector[start] = fnv1a(text, seed) / 0xffffffff - 0.5
  }
  let norm = 0
  for (const value of vector) norm += value * value
  norm = Math.sqrt(norm) || 1
  for (let index = 0; index < DIMENSIONS; index += 1) {
    vector[index] /= norm
  }
  return Array.from(vector, (value) => Number(value.toFixed(6)))
}

function readBody(request) {
  return new Promise((resolve, reject) => {
    const chunks = []
    request.on('data', (chunk) => chunks.push(chunk))
    request.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    request.on('error', reject)
  })
}

const server = createServer(async (request, response) => {
  const pathname = new URL(request.url, `http://${request.headers.host}`).pathname
  if (pathname === '/healthz') {
    response.writeHead(200, { 'content-type': 'text/plain; charset=utf-8' })
    response.end('ok')
    return
  }

  if (request.method !== 'POST' || pathname !== '/v1/embeddings') {
    response.writeHead(404, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ error: { message: `no route: ${request.method} ${pathname}` } }))
    return
  }

  let payload
  try {
    payload = JSON.parse(await readBody(request))
  } catch {
    response.writeHead(400, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ error: { message: 'invalid json body' } }))
    return
  }

  const inputs = Array.isArray(payload.input) ? payload.input : [String(payload.input ?? '')]
  const data = inputs.map((text, index) => ({
    object: 'embedding',
    index,
    embedding: embed(String(text)),
  }))
  response.writeHead(200, { 'content-type': 'application/json' })
  response.end(
    JSON.stringify({
      object: 'list',
      data,
      model: payload.model ?? 'stub-embedding',
      usage: { prompt_tokens: 0, total_tokens: 0 },
    })
  )
})

server.listen(PORT, HOST, () => {
  console.log(`[embeddings] stub listening on http://${HOST}:${PORT}/v1/embeddings`)
})
