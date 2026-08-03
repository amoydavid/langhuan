import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { invalidateGenerationExperience } from './cache'

describe('invalidateGenerationExperience', () => {
  it('refreshes generation history and knowledge-base summaries after mutations', async () => {
    const client = new QueryClient()
    const invalidateQueries = vi.spyOn(client, 'invalidateQueries')

    await invalidateGenerationExperience(client, 'acme', 'kb-id')

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['index-generations', 'acme', 'kb-id'],
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['knowledge-base-summary', 'acme', 'kb-id'],
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['knowledge-base', 'acme', 'kb-id'],
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['retrieval-test', 'acme', 'kb-id'],
    })
  })
})
