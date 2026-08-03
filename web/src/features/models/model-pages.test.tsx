import { describe, expect, it, vi } from 'vitest'
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
})
