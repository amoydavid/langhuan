import { AxiosError } from 'axios'
import { describe, expect, it } from 'vitest'
import { apiClient, resolveApiBaseURL } from './client'

describe('resolveApiBaseURL', () => {
  it('defaults to the versioned REST namespace', () => {
    expect(resolveApiBaseURL()).toBe('/api/v1')
  })

  it('normalizes trailing slashes', () => {
    expect(resolveApiBaseURL('/api/v1/')).toBe('/api/v1')
    expect(resolveApiBaseURL('https://langhuan.example.com/api/v1///')).toBe(
      'https://langhuan.example.com/api/v1'
    )
  })

  it('rejects bases outside the versioned REST namespace', () => {
    expect(() => resolveApiBaseURL('/api')).toThrow(
      'VITE_API_BASE_URL 必须以 /api/v1 结尾'
    )
  })
})

describe('apiClient', () => {
  it('uses HttpOnly-cookie credentials and the versioned base URL', () => {
    expect(apiClient.defaults.baseURL).toBe('/api/v1')
    expect(apiClient.defaults.withCredentials).toBe(true)
  })

  it('converts transport failures to ApiError', async () => {
    const request = apiClient.get('/broken', {
      adapter: async () => {
        throw new AxiosError('Network Error', 'ERR_NETWORK')
      },
    })

    await expect(request).rejects.toMatchObject({
      name: 'ApiError',
      status: 0,
      code: 'network_error',
    })
  })
})
