import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { invalidateSelectableModels } from './cache'

describe('invalidateSelectableModels', () => {
  it('targets one workspace for workspace mutations', async () => {
    const client = new QueryClient()
    const invalidateQueries = vi.spyOn(client, 'invalidateQueries')

    await invalidateSelectableModels(client, 'workspace', 'acme')

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['models', 'workspace', 'acme', 'selectable'],
    })
  })

  it('targets every workspace model cache for platform mutations', async () => {
    const client = new QueryClient()
    const invalidateQueries = vi.spyOn(client, 'invalidateQueries')

    await invalidateSelectableModels(client, 'platform')

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['models', 'workspace'],
    })
  })
})
