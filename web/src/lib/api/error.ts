import axios from 'axios'
import i18n from '@/lib/i18n'

type ErrorEnvelope = {
  error: {
    code: string
    message: string
  }
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isErrorEnvelope(value: unknown): value is ErrorEnvelope {
  if (!isRecord(value) || !isRecord(value.error)) return false
  return (
    typeof value.error.code === 'string' &&
    typeof value.error.message === 'string'
  )
}

/**
 * 按错误码取本地化文案。后端返回的 message 是服务端中文文案，
 * 不直接透传给用户，统一走 i18n 资源（errors.<code>）。
 * 未知错误码回退到 errors.unknown。
 */
export function localizedErrorMessage(code: string): string {
  const key = `errors.${code}`
  if (!i18n.exists(key)) {
    return i18n.t('errors.unknown')
  }
  // 动态 key 无法走类型安全重载，用 as never 逃生（key 已通过 exists 校验）
  return i18n.t(key as never)
}

export function parseApiError(error: unknown): ApiError {
  if (error instanceof ApiError) return error
  if (!axios.isAxiosError(error)) {
    return new ApiError(
      localizedErrorMessage('network_error'),
      0,
      'network_error'
    )
  }

  const status = error.response?.status ?? 0
  if (isErrorEnvelope(error.response?.data)) {
    const code = error.response.data.error.code
    return new ApiError(localizedErrorMessage(code), status, code)
  }
  if (status === 0) {
    return new ApiError(
      localizedErrorMessage('network_error'),
      0,
      'network_error'
    )
  }
  return new ApiError(localizedErrorMessage('http_error'), status, 'http_error')
}
