import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { CommandMenu } from './command-menu'

const mocks = vi.hoisted(() => ({
  listKnowledgeBases: vi.fn(),
  navigate: vi.fn(),
  setOpen: vi.fn(),
  setTheme: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => mocks.navigate,
  useParams: () => ({ workspaceSlug: 'acme', kbId: 'kb-current' }),
}))

vi.mock('@/context/search-provider', () => ({
  useSearch: () => ({ open: true, setOpen: mocks.setOpen }),
}))

vi.mock('@/context/theme-provider', () => ({
  useTheme: () => ({ setTheme: mocks.setTheme }),
}))

vi.mock('@/features/knowledge-bases/api', () => ({
  getKnowledgeBase: vi.fn(),
  listKnowledgeBases: mocks.listKnowledgeBases,
}))

const knowledgeBase = {
  id: 'kb-current',
  workspace_id: 'workspace-internal-id',
  name: '产品文档',
  description: '交付与运维资料',
  embedding_model_id: 'model-internal-id',
  embedding_model: {
    id: 'model-internal-id',
    name: 'embed',
    display_name: 'Embedding',
    provider: 'openai',
    provider_display_name: 'OpenAI',
    dimensions: 1024,
    available: true,
  },
  chunking_config: { chunk_size: 512, chunk_overlap: 80 },
  retrieval_config: {
    fts_config: 'simple',
    vector_top_k: 30,
    keyword_top_k: 30,
    final_top_k: 10,
    rrf_k: 60,
  },
  content_version: 3,
  active_index_generation_id: 'generation-internal-id',
  file_tree_root_id: 'tree-internal-id',
  metadata: {},
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
}

describe('CommandMenu', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.listKnowledgeBases.mockResolvedValue([knowledgeBase])
  })

  it('loads current Workspace knowledge bases without a pre-populated cache', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(['me'], {
      user: {
        id: 'user-internal-id',
        email: 'lin@example.com',
        nickname: '林墨',
        is_platform_admin: false,
      },
      workspaces: [
        {
          workspace_id: 'workspace-internal-id',
          slug: 'acme',
          name: '琅嬛研发',
          role: 'member',
        },
      ],
    })

    const screen = await render(
      <QueryClientProvider client={queryClient}>
        <CommandMenu />
      </QueryClientProvider>
    )

    await expect
      .poll(() => mocks.listKnowledgeBases)
      .toHaveBeenCalledWith('acme')
    await expect
      .element(screen.getByText('产品文档', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('创建知识库')).toBeVisible()
    await expect
      .element(screen.getByText('上传文件到「产品文档」'))
      .toBeVisible()
    await expect.element(screen.getByText('创建 FAQ')).toBeVisible()
    await expect.element(screen.getByText('打开检索测试')).toBeVisible()
    expect(document.body.textContent).not.toContain('workspace-internal-id')
  })
})
