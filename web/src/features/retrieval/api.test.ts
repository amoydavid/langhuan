import { describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/lib/api/client'
import { consoleSearchHeader, testRetrieval } from './api'

vi.mock('@/lib/api/client', () => ({
  apiClient: { post: vi.fn().mockResolvedValue({ data: [] }) },
}))

describe('testRetrieval 控制台检索标记（v1.4.0 §9.2）', () => {
  it('sends the console search marker header without touching the global client', async () => {
    await testRetrieval('acme', 'kb-1', { query: '如何配置 Docker？' })

    expect(apiClient.post).toHaveBeenCalledTimes(1)
    expect(apiClient.post).toHaveBeenCalledWith(
      '/workspaces/acme/knowledge-bases/kb-1/search',
      { query: '如何配置 Docker？' },
      { headers: { ...consoleSearchHeader } }
    )
  })
})
