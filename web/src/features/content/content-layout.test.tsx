import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { KnowledgeBaseSummary } from '@/features/knowledge-bases/workbench/types'
import { ContentLayout } from './content-layout'

const location = vi.hoisted(() => ({
  pathname: '/workspaces/acme/kb/kb-readable/content/all',
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    ...props
  }: React.ComponentProps<'a'> & { to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  Outlet: () => <div>内容叶子路由</div>,
  useLocation: () => location,
  useNavigate: () => vi.fn(),
  useMatches: () => [],
}))

const summary: KnowledgeBaseSummary = {
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  knowledge_base_name: '产品文档',
  content_version: 18,
  document_counts: {
    total: 20,
    file: 14,
    faq: 5,
    web: 1,
    ready: 18,
    processing: 1,
    failed: 1,
  },
  active_generation: null,
  candidate_generation: null,
  sync_state: 'updating',
  recent_jobs: [],
  blockers: [],
}

describe('ContentLayout', () => {
  beforeEach(() => {
    location.pathname = '/workspaces/acme/kb/kb-readable/content/all'
  })

  it('keeps type navigation in a nested Outlet with real counts', async () => {
    const screen = await render(
      <ContentLayout
        workspaceSlug='acme'
        kbId='kb-readable'
        summary={summary}
      />
    )

    for (const name of ['全部内容 20', '文件 14', 'FAQ 5', 'Web 1']) {
      await expect.element(screen.getByRole('link', { name })).toBeVisible()
    }
    await expect
      .element(screen.getByRole('combobox', { name: '内容类型' }))
      .toBeInTheDocument()
    await expect.element(screen.getByText('内容叶子路由')).toBeVisible()
  })

  it('keeps the Web tab visible even with no source and no web document (entry for the first source)', async () => {
    // Web tab 承载网站内容源的管理入口：按条件隐藏会让全新知识库没有任何
    // UI 入口创建第一个网站源（鸡生蛋死锁）。空态自身带角色门控。
    const screen = await render(
      <ContentLayout
        workspaceSlug='acme'
        kbId='kb-readable'
        summary={{
          ...summary,
          document_counts: { ...summary.document_counts, web: 0 },
        }}
      />
    )
    await expect
      .element(screen.getByRole('link', { name: 'Web 0' }))
      .toBeVisible()
  })

  it('keeps the Web tab visible when a source exists with zero web documents', async () => {
    const screen = await render(
      <ContentLayout
        workspaceSlug='acme'
        kbId='kb-readable'
        summary={{
          ...summary,
          document_counts: { ...summary.document_counts, web: 0 },
        }}
        webSourceCount={2}
      />
    )
    // 计数文案以「源」为单位（spec §9.5.1）。
    await expect
      .element(screen.getByRole('link', { name: 'Web 2 个源' }))
      .toBeVisible()
  })

  it('falls back to the document count when the source list has not loaded', async () => {
    const screen = await render(
      <ContentLayout
        workspaceSlug='acme'
        kbId='kb-readable'
        summary={summary}
      />
    )
    await expect
      .element(screen.getByRole('link', { name: 'Web 1' }))
      .toBeVisible()
  })

  it('derives the active type from the nested route instead of local tab state', async () => {
    location.pathname =
      '/workspaces/acme/kb/kb-readable/content/files/document-readable'
    const screen = await render(
      <ContentLayout
        workspaceSlug='acme'
        kbId='kb-readable'
        summary={summary}
      />
    )
    await expect
      .element(screen.getByRole('link', { name: '文件 14' }))
      .toHaveAttribute('aria-current', 'page')
    await expect
      .element(screen.getByRole('link', { name: '全部内容 20' }))
      .not.toHaveAttribute('aria-current')
  })
})
