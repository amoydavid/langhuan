import axios from 'axios'
import i18n from '@/lib/i18n'
import { parseApiError } from './error'

const apiTimeoutMs = 15_000

export function resolveApiBaseURL(
  raw: string | undefined = import.meta.env.VITE_API_BASE_URL
) {
  const value = (raw || '/api/v1').replace(/\/+$/, '')
  if (!value.endsWith('/api/v1')) {
    throw new Error(i18n.t('common.invalidApiBaseUrl'))
  }
  return value
}

export const apiClient = axios.create({
  baseURL: resolveApiBaseURL(),
  withCredentials: true,
  timeout: apiTimeoutMs,
})

apiClient.interceptors.response.use(
  (response) => response,
  (error: unknown) => Promise.reject(parseApiError(error))
)
