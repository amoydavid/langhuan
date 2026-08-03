import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { WorkspaceReadiness } from './types'
import { WorkspaceReadinessPanel } from './workspace-readiness-panel'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}))

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const documentId = '087a124b-859f-4786-902a-2dd1901a006f'

const readiness: WorkspaceReadiness = {
  has_active_provider: true,
  has_selectable_embedding_model: true,
  knowledge_base_count: 2,
  document_counts: { total: 20, ready: 18, processing: 1, failed: 1 },
  searchable_knowledge_base_count: 1,
  recommended_action: 'resolve_failed_document',
  recommended_knowledge_base_id: kbId,
  recommended_knowledge_base_name: '产品文档',
  recommended_document_id: documentId,
  recommended_document_name: 'faq-import.csv',
}

describe('WorkspaceReadinessPanel', () => {
  it('shows the truthful next action, counts and readable KnowledgeBase names', async () => {
    const screen = await render(
      <WorkspaceReadinessPanel
        workspaceSlug='acme'
        readiness={readiness}
        knowledgeBases={[
          { id: kbId, name: '产品文档', description: '交付与运维资料' },
          {
            id: '27dee408-151f-4bda-b605-e2df7e598593',
            name: '客服 FAQ',
            description: '客服标准答复',
          },
        ]}
        canManageWorkspace
        canManageInvitations
      />
    )

    await expect.element(screen.getByText('处理失败内容')).toBeVisible()
    await expect.element(screen.getByText('faq-import.csv')).toBeVisible()
    await expect.element(screen.getByText('18 条可检索')).toBeVisible()
    await expect
      .element(screen.getByText('产品文档', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('客服 FAQ')).toBeVisible()
    await expect.element(screen.getByText('成员与邀请')).toBeVisible()
    expect(document.body.textContent).not.toContain(kbId)
    expect(document.body.textContent).not.toContain(documentId)
  })

  it('explains an administrator-only setup action to a member', async () => {
    const screen = await render(
      <WorkspaceReadinessPanel
        workspaceSlug='acme'
        readiness={{
          ...readiness,
          has_active_provider: false,
          has_selectable_embedding_model: false,
          knowledge_base_count: 0,
          document_counts: { total: 0, ready: 0, processing: 0, failed: 0 },
          searchable_knowledge_base_count: 0,
          recommended_action: 'configure_provider',
          recommended_knowledge_base_id: null,
          recommended_knowledge_base_name: '',
          recommended_document_id: null,
          recommended_document_name: '',
        }}
        knowledgeBases={[]}
        canManageWorkspace={false}
        canManageInvitations={false}
      />
    )

    await expect
      .element(screen.getByText('请联系 Workspace 管理员配置模型连接。'))
      .toBeVisible()
    await expect
      .element(screen.getByText('配置模型连接', { exact: true }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('成员与邀请', { exact: true }))
      .not.toBeInTheDocument()
  })
})
