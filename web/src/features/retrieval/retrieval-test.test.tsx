import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import {
  RetrievalTest,
  retrievalSearchSchema,
  toRetrievalRequest,
} from './retrieval-test'
import type { RetrievalRequest, RetrievalResult } from './types'

const kbId = '20000000-0000-4000-8000-000000000002'
const documentId = '30000000-0000-4000-8000-000000000003'
const chunkId = '40000000-0000-4000-8000-000000000004'
const matchedChildId = '60000000-0000-4000-8000-000000000006'

const defaults = {
  fts_config: 'simple',
  vector_top_k: 20,
  keyword_top_k: 20,
  final_top_k: 8,
  rrf_k: 60,
}

const results: RetrievalResult[] = [
  {
    chunk_id: chunkId,
    chunk_revision_id: '50000000-0000-4000-8000-000000000005',
    document_id: documentId,
    document_kind: 'file',
    document_name: 'installation.md',
    content: 'Docker 部署时，通过 DATABASE_DSN 指定 PostgreSQL。',
    source_anchor: { line_start: 24, line_end: 31 },
    score: 0.0325,
    vector_score: 0.84,
    keyword_score: 12.31,
    metadata: { heading: 'Docker 部署', internal_hash: 'do-not-render' },
    matched_children: [
      {
        chunk_id: matchedChildId,
        chunk_revision_id: '70000000-0000-4000-8000-000000000007',
        role: 'child',
        content: '通过 DATABASE_DSN 指定 PostgreSQL。',
        source_anchor: { line_start: 24, line_end: 31 },
        score: 0.0325,
        vector_score: 0.84,
        keyword_score: 12.31,
      },
    ],
  },
]

// stubResults 注入一个返回固定结果的 useResults，避免浏览器 mock 不稳定。
function stubResults() {
  return (_ws: string, _kb: string, _req: RetrievalRequest | null) => ({
    data: results,
    isFetching: false,
    error: null,
  })
}

describe('RetrievalTest', () => {
  it('coerces typed URL params so direct/F5 routes replay the same request', () => {
    const search = retrievalSearchSchema.parse({
      q: ' Docker 配置 ',
      vectorTopK: '12',
      keywordTopK: '9',
      finalTopK: '5',
      chunk: chunkId,
    })

    expect(search).toEqual({
      q: 'Docker 配置',
      vectorTopK: 12,
      keywordTopK: 9,
      finalTopK: 5,
      chunk: chunkId,
    })
    expect(toRetrievalRequest(search, defaults)).toEqual({
      query: 'Docker 配置',
      vector_top_k: 12,
      keyword_top_k: 9,
      final_top_k: 5,
    })
  })

  it('runs search via AJAX (no URL navigation) and shows results', async () => {
    const useResults = vi.fn(stubResults())
    const screen = await render(
      <RetrievalTest
        workspaceSlug='acme'
        kbId={kbId}
        search={{}}
        defaults={defaults}
        activeGenerationLabel='2026-08-01 09:42 · 当前生效'
        useResults={useResults}
      />
    )

    await userEvent.fill(screen.getByLabelText('检索问题'), '如何配置 Docker？')
    await userEvent.click(screen.getByRole('button', { name: '检索' }))

    // 点击后 useResults 被调用，结果通过 AJAX 出现，不触发任何路由导航。
    expect(useResults).toHaveBeenCalled()
    await expect
      .element(
        screen.getByText(
          '分数仅用于本次结果排序，不是相关度百分比或答案可信度。'
        )
      )
      .toBeVisible()
    await expect.element(screen.getByText('RRF 0.0325').first()).toBeVisible()
    await expect.element(screen.getByText('返回完整上下文')).toBeVisible()
    await expect.element(screen.getByText('命中片段 1')).toBeVisible()
    expect(document.body.textContent).not.toContain('3.25%')
  })

  it('renders readable source anchors and canonical source/chunk deep links without bare IDs', async () => {
    const screen = await render(
      <RetrievalTest
        workspaceSlug='acme'
        kbId={kbId}
        search={{ q: 'Docker' }}
        defaults={defaults}
        useResults={stubResults()}
      />
    )

    await expect.element(screen.getByText('第 24–31 行').first()).toBeVisible()
    await expect.element(screen.getByText('Docker 部署')).toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: '查看来源' }))
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/files/${documentId}`
      )
    await expect
      .element(screen.getByRole('link', { name: '打开分块' }))
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/files/${documentId}?chunk=${matchedChildId}&anchor=1`
      )
    expect(document.body.textContent).not.toContain(kbId)
    expect(document.body.textContent).not.toContain(documentId)
    expect(document.body.textContent).not.toContain(chunkId)
    expect(document.body.textContent).not.toContain(matchedChildId)
    expect(document.body.textContent).not.toContain('do-not-render')
  })
})
