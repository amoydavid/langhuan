import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ApiError } from '@/lib/api/error'
import {
  DOCUMENT_ACCEPT,
  DocumentUploadForm,
  documentUploadErrorMessage,
} from './document-upload-form'

const navigate = vi.hoisted(() => vi.fn())
const post = vi.hoisted(() => vi.fn())
const toastSuccess = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})
vi.mock('@/lib/api/client', () => ({ apiClient: { post } }))
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }))

const document = {
  id: '30000000-0000-0000-0000-000000000003',
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-0000-0000-000000000002',
  kind: 'file' as const,
  title: 'guide.md',
  source_type: 'upload',
  source_uri: null,
  status: 'pending' as const,
  normalized_markdown: '',
  metadata: {},
  error_message: '',
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
  active_revision: {
    id: '50000000-0000-4000-8000-000000000005',
    revision_no: 1,
    status: 'pending' as const,
    original_filename: 'guide.md',
    file_type: 'markdown',
    content_type: 'text/markdown',
    sha256: 'abc123',
    size_bytes: 5,
    created_at: '2026-07-30T00:00:00Z',
  },
}

const job = {
  id: '40000000-0000-0000-0000-000000000004',
  document_id: document.id,
  type: 'document_parse_start',
  status: 'queued' as const,
  attempts: 0,
  external_job_id: '',
  payload: {},
  error_message: '',
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

async function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateQueries = vi.spyOn(client, 'invalidateQueries')
  const screen = await render(
    <QueryClientProvider client={client}>
      <DocumentUploadForm
        workspaceSlug='acme'
        kbId={document.knowledge_base_id}
        parentNodeId='60000000-0000-4000-8000-000000000006'
      />
    </QueryClientProvider>
  )
  return { screen, invalidateQueries }
}

describe('DocumentUploadForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    post.mockResolvedValue({ data: { document, job, deduped: true } })
  })

  it('uses the exact supported-file accept contract', async () => {
    const { screen } = await renderForm()
    expect(DOCUMENT_ACCEPT).toBe('.pdf,.md,.markdown,.txt,.csv,.xlsx,.docx')
    await expect
      .element(screen.getByLabelText('文件', { exact: true }))
      .toHaveAttribute('accept', DOCUMENT_ACCEPT)
  })

  it('sends multipart fields with dedupe only in the query string', async () => {
    const { screen, invalidateQueries } = await renderForm()
    await expect
      .element(screen.getByLabelText('Metadata'))
      .not.toBeInTheDocument()
    const file = new File(['hello'], 'guide.md', { type: 'text/markdown' })
    await userEvent.upload(screen.getByLabelText('文件', { exact: true }), file)
    await expect.element(screen.getByLabelText('标题')).toHaveValue('guide.md')
    await userEvent.click(
      screen.getByRole('button', { name: '上传并查看处理状态' })
    )

    await vi.waitFor(() => expect(post).toHaveBeenCalledOnce())
    const [path, body, config] = post.mock.calls[0]
    expect(path).toBe(
      `/workspaces/acme/knowledge-bases/${document.knowledge_base_id}/documents`
    )
    expect(body).toBeInstanceOf(FormData)
    const uploadedFile = (body as FormData).get('file')
    expect(uploadedFile).toBeInstanceOf(File)
    expect((uploadedFile as File).name).toBe('guide.md')
    expect((uploadedFile as File).type).toBe('text/markdown')
    expect((body as FormData).get('title')).toBe('guide.md')
    expect((body as FormData).get('source_type')).toBe('upload')
    expect((body as FormData).get('parent_node_id')).toBe(
      '60000000-0000-4000-8000-000000000006'
    )
    expect((body as FormData).get('node_name')).toBe('guide.md')
    expect((body as FormData).has('metadata')).toBe(false)
    expect((body as FormData).has('dedupe')).toBe(false)
    expect(config).toEqual({ params: { dedupe: true } })
    expect(config).not.toHaveProperty('headers')
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['documents', 'acme', document.knowledge_base_id],
    })
  })

  it('shows reuse feedback and keeps the response job link', async () => {
    const { screen } = await renderForm()
    await userEvent.upload(
      screen.getByLabelText('文件', { exact: true }),
      new File(['hello'], 'guide.md', { type: 'text/markdown' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: '上传并查看处理状态' })
    )

    await vi.waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith('已复用已有文档')
    )
    expect(navigate).toHaveBeenCalledWith({
      to: '/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId',
      params: {
        workspaceSlug: 'acme',
        kbId: document.knowledge_base_id,
        documentId: document.id,
      },
      search: { job: job.id },
      replace: true,
    })
  })

  it('distinguishes size limits from unsupported file types', () => {
    expect(
      documentUploadErrorMessage(
        new ApiError('file 超过大小限制', 413, 'validation_error')
      )
    ).toBe('文件超过服务端大小限制，请选择更小的文件')
    expect(
      documentUploadErrorMessage(
        new ApiError('不支持的文件类型', 415, 'unsupported_file_type')
      )
    ).toBe('当前文件类型不受支持，请选择支持的格式')
  })
})
