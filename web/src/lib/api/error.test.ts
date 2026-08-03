import { AxiosError, type AxiosResponse } from 'axios'
import { describe, expect, it } from 'vitest'
import { ApiError, parseApiError } from './error'

function axiosError(status: number, data: unknown) {
  return new AxiosError(
    'Request failed',
    'ERR_BAD_RESPONSE',
    undefined,
    undefined,
    { status, data } as AxiosResponse
  )
}

describe('parseApiError', () => {
  it('reads the backend error envelope', () => {
    expect(
      parseApiError(
        axiosError(409, {
          error: { code: 'conflict', message: 'slug 已存在' },
        })
      )
    ).toMatchObject({
      status: 409,
      code: 'conflict',
      // 后端 message 不透传：按 code 映射为本地化文案
      message: '资源冲突',
    })
  })

  it('falls back to the generic message for unknown error codes', () => {
    expect(
      parseApiError(
        axiosError(422, {
          error: { code: 'future_code_unknown', message: '原始中文细节' },
        })
      )
    ).toMatchObject({
      status: 422,
      code: 'future_code_unknown',
      message: '操作失败，请稍后重试',
    })
  })

  it('uses a stable HTTP fallback when the envelope is absent', () => {
    expect(
      parseApiError(axiosError(500, { title: 'upstream details' }))
    ).toMatchObject({
      status: 500,
      code: 'http_error',
      message: '请求失败，请稍后重试',
    })
  })

  it('uses a stable network fallback for unknown errors', () => {
    expect(parseApiError(new Error('socket details'))).toMatchObject({
      status: 0,
      code: 'network_error',
      message: '网络连接失败，请检查后重试',
    })
  })

  it('does not wrap ApiError twice', () => {
    const original = new ApiError('冲突', 409, 'conflict')
    expect(parseApiError(original)).toBe(original)
  })
})
