import { apiClient } from '@/lib/api/client'
import type {
  AuthenticatedUser,
  BootstrapStatus,
  ExternalIdentity,
  LoginInput,
  LoginResponse,
  MeResponse,
  PublicInvitation,
  RegisterInput,
} from './types'

export async function login(input: LoginInput) {
  const response = await apiClient.post<LoginResponse>('/auth/login', input)
  return response.data
}

export async function logout() {
  await apiClient.post('/auth/logout')
}

export async function getMe() {
  const response = await apiClient.get<MeResponse>('/auth/me')
  return response.data
}

export async function getBootstrapStatus() {
  const response = await apiClient.get<BootstrapStatus>(
    '/auth/bootstrap-status'
  )
  return response.data
}

export async function registerUser(input: RegisterInput) {
  const response = await apiClient.post<AuthenticatedUser>(
    '/auth/register',
    input
  )
  return response.data
}

export async function getPublicInvitation(token: string) {
  const response = await apiClient.get<PublicInvitation>(
    `/invitations/${encodeURIComponent(token)}`
  )
  return response.data
}

/**
 * OIDC 登录入口。跳转到后端 /auth/oidc/login，后端 302 到 IdP。
 * next 为登录后跳转路径；invitationToken 用于邀请接受流程。
 */
export function startOIDCLogin(opts?: {
  next?: string
  invitationToken?: string
  bind?: boolean
}) {
  const params = new URLSearchParams()
  if (opts?.next) params.set('next', opts.next)
  if (opts?.invitationToken)
    params.set('invitation_token', opts.invitationToken)
  const qs = params.toString()
  const base = opts?.bind ? '/auth/oidc/bind/start' : '/auth/oidc/login'
  window.location.href = `/api/v1${base}${qs ? `?${qs}` : ''}`
}

/**
 * 发起 OIDC 绑定（已登录用户）。用 POST 触发（SameSite=Lax session cookie 随跨站 POST 不发送）。
 */
export async function startOIDCBind() {
  // bind/start 是 POST + 302；用表单提交确保 session cookie 携带。
  const form = document.createElement('form')
  form.method = 'POST'
  form.action = '/api/v1/auth/oidc/bind/start'
  document.body.appendChild(form)
  form.submit()
}

/**
 * 获取当前用户已绑定的外部身份（非敏感摘要）。
 */
export async function getExternalIdentities() {
  const response = await apiClient.get<{ identities: ExternalIdentity[] }>(
    '/auth/external-identities'
  )
  return response.data.identities
}
