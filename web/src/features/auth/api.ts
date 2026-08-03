import { apiClient } from '@/lib/api/client'
import type {
  AuthenticatedUser,
  BootstrapStatus,
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
