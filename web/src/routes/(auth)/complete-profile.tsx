import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'
import { CompleteProfile } from '@/features/auth/complete-profile'
import { meQueryOptions } from '@/features/auth/queries'
import { parseApiError } from '@/lib/api/error'

const searchSchema = z.object({
  next: z.string().optional(),
  invitation_token_hash: z.string().optional(),
})

export const Route = createFileRoute('/(auth)/complete-profile')({
  validateSearch: searchSchema,
  beforeLoad: async ({ context }) => {
    // 需要已登录（session cookie 由 OIDC callback 设置）。
    try {
      await context.queryClient.ensureQueryData(meQueryOptions())
    } catch (error) {
      if (parseApiError(error).status === 401) {
        throw redirect({ to: '/sign-in', replace: true })
      }
      throw error
    }
  },
  component: CompleteProfile,
})
