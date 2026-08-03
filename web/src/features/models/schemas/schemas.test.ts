import { describe, expect, it } from 'vitest'
import { embeddingDimensions, modelFormDefaults } from '../types'
import { providerFormSchema } from '.'

describe('model configuration schemas', () => {
  it('uses exactly the indexed dimensions and defaults to 1024', () => {
    expect(embeddingDimensions).toEqual([798, 1024, 2048, 3584])
    expect(modelFormDefaults.dimensions).toBe(1024)
  })

  it('enforces provider-specific credential and scope contracts', () => {
    const common = {
      name: 'production',
      display_name: 'Production',
      description: '',
      timeout_seconds: 60,
      replace_credentials: true,
    }
    expect(
      providerFormSchema.safeParse({
        ...common,
        scope: 'workspace',
        provider: 'openai',
        mode: 'azure',
        base_url: '',
        api_version: '',
        api_key: 'secret',
      }).success
    ).toBe(false)
    expect(
      providerFormSchema.safeParse({
        ...common,
        scope: 'workspace',
        provider: 'openai',
        mode: 'standard',
        base_url: '',
        api_version: '',
        api_key: 'secret',
        custom_headers: 'missing-colon',
      }).success
    ).toBe(false)
    expect(
      providerFormSchema.safeParse({
        ...common,
        scope: 'workspace',
        provider: 'ark',
        auth_mode: 'ak_sk',
        base_url: '',
        region: 'cn-beijing',
        retry_times: 2,
        access_key: 'ak',
        secret_key: 'sk',
      }).success
    ).toBe(true)
    expect(
      providerFormSchema.safeParse({
        ...common,
        scope: 'workspace',
        provider: 'ollama',
        base_url: 'http://localhost:11434',
      }).success
    ).toBe(false)
  })
})
