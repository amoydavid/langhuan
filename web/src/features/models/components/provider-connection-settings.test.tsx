import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { ModelProvider } from '../types'
import { ProviderConnectionSettings } from './provider-connection-settings'

describe('ProviderConnectionSettings', () => {
  it('renders only readable allowlisted configuration', async () => {
    const provider: ModelProvider = {
      id: 'secret-uuid',
      scope: 'platform',
      name: 'siliconflow',
      display_name: 'SiliconFlow',
      description: '',
      provider: 'siliconflow',
      config: {
        base_url: 'https://api.siliconflow.cn',
        config_hash: 'hidden-hash',
        nested: { raw: true },
      },
      credentials_configured: true,
      credential_fields: ['api_key'],
      capabilities: ['embedding', 'rerank'],
      model_counts: { total: 0, active: 0, embedding: 0, rerank: 0 },
      status: 'active',
      created_at: '',
      updated_at: '',
    }
    const screen = await render(
      <ProviderConnectionSettings provider={provider} />
    )
    await expect
      .element(screen.getByText('https://api.siliconflow.cn'))
      .toBeVisible()
    await expect
      .element(screen.getByText('hidden-hash'))
      .not.toBeInTheDocument()
    await expect.element(screen.getByText(/raw/)).not.toBeInTheDocument()
  })
})
