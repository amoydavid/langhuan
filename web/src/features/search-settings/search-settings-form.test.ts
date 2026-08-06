import { describe, expect, it } from 'vitest'
import { searchSettingsFormSchema } from './search-settings-form'

describe('search settings form schema', () => {
  it('requires a model when global Rerank is enabled', () => {
    expect(
      searchSettingsFormSchema.safeParse({
        rerank_enabled: true,
        candidate_top_k: 50,
        failure_mode: 'fallback',
      }).success
    ).toBe(false)
  })

  it('allows disabling Rerank without a model', () => {
    expect(
      searchSettingsFormSchema.safeParse({
        rerank_enabled: false,
        candidate_top_k: 50,
        failure_mode: 'fallback',
      }).success
    ).toBe(true)
  })

  it('rejects candidate counts outside the backend contract', () => {
    expect(
      searchSettingsFormSchema.safeParse({
        rerank_enabled: true,
        rerank_model_id: '20000000-0000-4000-8000-000000000002',
        candidate_top_k: 201,
        failure_mode: 'fail',
      }).success
    ).toBe(false)
  })
})
