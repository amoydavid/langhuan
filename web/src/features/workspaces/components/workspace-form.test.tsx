import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { workspaceSchema } from '../schemas'
import { WorkspaceForm } from './workspace-form'

const navigate = vi.hoisted(() => vi.fn())
const createWorkspace = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})
vi.mock('../api', () => ({ createWorkspace }))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

async function renderForm(isPlatformAdmin = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateQueries = vi.spyOn(client, 'invalidateQueries')
  const screen = await render(
    <QueryClientProvider client={client}>
      <WorkspaceForm isPlatformAdmin={isPlatformAdmin} />
    </QueryClientProvider>
  )
  return { screen, invalidateQueries }
}

describe('WorkspaceForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createWorkspace.mockResolvedValue({
      id: '10000000-0000-0000-0000-000000000001',
      name: 'Acme',
      slug: 'acme',
      metadata: {},
      created_at: '2026-07-30T00:00:00Z',
      updated_at: '2026-07-30T00:00:00Z',
    })
  })

  it('validates name and the backend slug grammar', () => {
    expect(workspaceSchema.safeParse({ name: '  ', slug: 'ab' }).success).toBe(
      false
    )
    expect(
      workspaceSchema.safeParse({
        name: 'Acme',
        slug: '-acme-',
      }).success
    ).toBe(false)
    expect(
      workspaceSchema.safeParse({
        name: 'Acme',
        slug: 'acme-01',
      }).success
    ).toBe(true)
  })

  it('does not expose workspace creation to non-platform administrators', async () => {
    const { screen } = await renderForm(false)

    await expect
      .element(screen.getByText('仅平台管理员可以创建工作区'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '创建工作区' }))
      .not.toBeInTheDocument()
  })

  it('creates a workspace without exposing metadata, refreshes me, and enters its knowledge bases', async () => {
    const { screen, invalidateQueries } = await renderForm()
    await expect
      .element(screen.getByLabelText('Metadata'))
      .not.toBeInTheDocument()
    await userEvent.fill(screen.getByLabelText('名称'), 'Acme')
    await userEvent.fill(screen.getByLabelText('Slug'), 'acme')
    await userEvent.click(screen.getByRole('button', { name: '创建工作区' }))

    await vi.waitFor(() => expect(createWorkspace).toHaveBeenCalledOnce())
    expect(createWorkspace).toHaveBeenCalledWith({
      name: 'Acme',
      slug: 'acme',
    })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['me'] })
    expect(navigate).toHaveBeenCalledWith({
      to: '/workspaces/acme/kb',
      replace: true,
    })
  })
})
