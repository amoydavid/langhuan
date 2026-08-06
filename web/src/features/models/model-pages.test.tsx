import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ModelProviderDetailContent } from './model-provider-detail-page'
import type { ModelProvider } from './types'

vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

const provider: ModelProvider = {
  id: 'provider-1',
  scope: 'platform',
  name: 'shared-openai',
  display_name: '平台 OpenAI',
  description: '',
  provider: 'openai',
  config: { mode: 'standard', timeout_seconds: 60 },
  credentials_configured: true,
  credential_fields: ['api_key'],
  capabilities: ['embedding'],
  model_counts: { total: 0, active: 0, embedding: 0, rerank: 0 },
  status: 'active',
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

describe('ModelProviderDetailContent', () => {
  it('keeps a platform provider read-only inside a workspace', async () => {
    const screen = await render(
      <ModelProviderDetailContent
        provider={provider}
        models={[]}
        routeScope='workspace'
        workspaceSlug='acme'
        canManage
      />
    )

    await expect
      .element(screen.getByText('平台共享（只读）'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '编辑连接' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '添加模型' }))
      .not.toBeInTheDocument()
  })

  it('opens on models and keeps connection settings behind its tab', async () => {
    const screen = await render(
      <ModelProviderDetailContent
        provider={provider}
        models={[]}
        routeScope='platform'
        canManage
      />
    )
    await expect.element(screen.getByText('此连接下还没有模型。')).toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: '模型' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: 'Embedding 模型' }))
      .not.toBeInTheDocument()
    await expect.element(screen.getByText('连接配置')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '连接设置' }))
    await expect.element(screen.getByText('连接配置')).toBeVisible()
  })
})
