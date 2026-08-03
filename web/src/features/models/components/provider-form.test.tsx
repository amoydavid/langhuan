import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { ModelProvider } from '../types'
import { ProviderForm } from './provider-form'

const createModelProvider = vi.hoisted(() => vi.fn())
const updateModelProvider = vi.hoisted(() => vi.fn())

vi.mock('../api', () => ({
  createModelProvider,
  updateModelProvider,
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

async function renderProviderForm() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const screen = await render(
    <QueryClientProvider client={queryClient}>
      <ProviderForm scope='workspace' workspaceSlug='acme' />
    </QueryClientProvider>
  )
  return { screen, queryClient }
}

const provider: ModelProvider = {
  id: 'provider-1',
  scope: 'workspace',
  workspace_id: 'workspace-1',
  name: 'openai-prod',
  display_name: 'OpenAI Production',
  description: '',
  provider: 'openai',
  config: { mode: 'standard', timeout_seconds: 60 },
  credentials_configured: true,
  credential_fields: ['api_key'],
  status: 'active',
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

describe('ProviderForm credential lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createModelProvider.mockResolvedValue({ id: 'provider-1' })
  })

  it('does not retain credentials in TanStack mutation state', async () => {
    const { screen, queryClient } = await renderProviderForm()
    await userEvent.fill(screen.getByLabelText('连接标识'), 'openai-prod')
    await userEvent.fill(screen.getByLabelText('显示名称'), 'OpenAI Production')
    await userEvent.fill(screen.getByLabelText('API Key'), 'top-secret')
    await userEvent.fill(
      screen.getByLabelText('自定义请求头'),
      'X-Tenant: acme'
    )
    await userEvent.click(screen.getByRole('button', { name: '保存连接' }))

    await vi.waitFor(() => expect(createModelProvider).toHaveBeenCalledOnce())
    expect(createModelProvider).toHaveBeenCalledWith(
      'workspace',
      expect.objectContaining({
        credentials: {
          api_key: 'top-secret',
          custom_headers: { 'X-Tenant': 'acme' },
        },
      }),
      'acme'
    )
    const mutations = queryClient.getMutationCache().getAll()
    const mutation = mutations[mutations.length - 1]
    expect(mutation?.state.variables).toBeUndefined()
    await expect.element(screen.getByLabelText('API Key')).toHaveValue('')
    await expect.element(screen.getByLabelText('自定义请求头')).toHaveValue('')
    expect(JSON.stringify(queryClient.getQueryCache().getAll())).not.toContain(
      'top-secret'
    )
  })

  it('invalidates the edited provider detail cache', async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries')
    updateModelProvider.mockResolvedValue(provider)
    const screen = await render(
      <QueryClientProvider client={queryClient}>
        <ProviderForm
          scope='workspace'
          workspaceSlug='acme'
          provider={provider}
        />
      </QueryClientProvider>
    )

    await userEvent.click(screen.getByRole('button', { name: '保存连接' }))

    await vi.waitFor(() => expect(updateModelProvider).toHaveBeenCalledOnce())
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['model-provider', 'workspace', 'acme', 'provider-1'],
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['models', 'workspace', 'acme', 'selectable'],
    })
  })
})
