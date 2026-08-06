import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { ModelProvider } from '../types'
import { ProviderCard } from './provider-card'

const provider: ModelProvider = {
  id: 'provider-1',
  scope: 'platform',
  name: 'openai-platform',
  display_name: '平台 OpenAI',
  description: '共享连接',
  provider: 'openai',
  config: { mode: 'standard' },
  credentials_configured: true,
  credential_fields: ['api_key'],
  capabilities: ['embedding'],
  model_counts: { total: 0, active: 0, embedding: 0, rerank: 0 },
  status: 'active',
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

describe('ProviderCard', () => {
  it('marks a shared provider as read-only on a workspace route', async () => {
    const screen = await render(
      <ProviderCard
        provider={provider}
        routeScope='workspace'
        workspaceSlug='acme'
      />
    )

    await expect.element(screen.getByText('平台共享')).toBeInTheDocument()
    await expect
      .element(screen.getByRole('link', { name: '查看连接' }))
      .toHaveAttribute('href', '/workspaces/acme/models/provider-1')
  })
})
