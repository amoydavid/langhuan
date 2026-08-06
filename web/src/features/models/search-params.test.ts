import { describe, expect, it } from 'vitest'
import { modelServiceSearchSchema } from './search-params'

describe('model service search params', () => {
  it('defaults to the model catalog', () => {
    expect(modelServiceSearchSchema.parse({})).toEqual({
      view: 'models',
      type: 'all',
      capability: 'all',
      status: 'all',
      scope: 'all',
      q: '',
    })
  })

  it('falls back from invalid values', () => {
    expect(
      modelServiceSearchSchema.parse({ view: 'unknown', type: 'llm', q: 42 })
    ).toMatchObject({ view: 'models', type: 'all', q: '' })
  })
})
