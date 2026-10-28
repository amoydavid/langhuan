import type * as React from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'

// Link 的最小 <a> 替身（项目既有惯例，见 content-activity/web-source-card 测试）。
vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    params,
    ...props
  }: React.ComponentProps<'a'> & {
    to: string
    params?: Record<string, string>
  }) => (
    <a
      href={Object.entries(params ?? {}).reduce<string>(
        (acc, [key, value]) => acc.replace(`$${key}`, value),
        to
      )}
      {...props}
    >
      {children}
    </a>
  ),
}))

import type { ModelProvider } from '../types'
import { ProviderCatalog } from './provider-catalog'

const provider: ModelProvider = {
  id: 'provider-1',
  scope: 'platform',
  name: 'siliconflow',
  display_name: 'SiliconFlow 平台',
  description: '',
  provider: 'siliconflow',
  config: {},
  credentials_configured: true,
  credential_fields: ['api_key'],
  capabilities: ['embedding', 'rerank'],
  model_counts: { total: 5, active: 4, embedding: 3, rerank: 2 },
  status: 'active',
  created_at: '2026-08-06T00:00:00Z',
  updated_at: '2026-08-06T00:00:00Z',
}

describe('ProviderCatalog', () => {
  it('shows server capabilities, counts and credential state', async () => {
    const screen = await render(
      <ProviderCatalog providers={[provider]} routeScope='platform' />
    )
    await expect.element(screen.getByText('Embedding')).toBeVisible()
    await expect.element(screen.getByText('Rerank')).toBeVisible()
    await expect.element(screen.getByText('5')).toBeVisible()
    await expect.element(screen.getByText('凭证已配置')).toBeVisible()
  })
})
