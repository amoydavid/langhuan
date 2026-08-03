import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { Model } from '@/features/models/types'
import { EmbeddingModelSelect } from './embedding-model-select'

const models = [
  {
    id: 'model-workspace',
    display_name: 'Workspace Embedding',
    dimensions: 1024,
    provider: { scope: 'workspace', display_name: 'Workspace OpenAI' },
  },
  {
    id: 'model-platform',
    display_name: 'Platform Embedding',
    dimensions: 2048,
    provider: { scope: 'platform', display_name: 'Platform ARK' },
  },
] as Model[]

describe('EmbeddingModelSelect', () => {
  it('groups workspace and platform models', async () => {
    const screen = await render(
      <EmbeddingModelSelect
        workspaceSlug='acme'
        workspaceRole='member'
        models={models}
        value=''
        onChange={vi.fn()}
      />
    )

    await expect.element(screen.getByText('当前 Workspace')).toBeInTheDocument()
    await expect.element(screen.getByText('平台共享')).toBeInTheDocument()
    await expect
      .element(screen.getByRole('option', { name: /Workspace Embedding/ }))
      .toHaveTextContent('1024 维')
  })
})
