import type * as React from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'

// Link 的最小 <a> 替身（项目既有惯例，见 content-activity/web-source-card 测试）：
// 替换 $params 并把 search 拼为 querystring，保持 href 断言语义。
vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    params,
    search,
    ...props
  }: React.ComponentProps<'a'> & {
    to: string
    params?: Record<string, string>
    search?: Record<string, string | number | undefined>
  }) => (
    <a
      href={
        Object.entries(params ?? {}).reduce<string>(
          (acc, [key, value]) => acc.replace(`$${key}`, value),
          to
        ) +
        (search
          ? `?${new URLSearchParams(
              Object.entries(search).filter(([, v]) => v !== undefined) as [
                string,
                string,
              ][]
            ).toString()}`
          : '')
      }
      {...props}
    >
      {children}
    </a>
  ),
}))

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
