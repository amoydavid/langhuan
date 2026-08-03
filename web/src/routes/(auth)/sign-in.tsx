import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'
import {
  chooseWorkspaceEntry,
  LAST_WORKSPACE_SLUG_KEY,
} from '@/features/auth/navigation'
import {
  bootstrapStatusQueryOptions,
  meQueryOptions,
} from '@/features/auth/queries'
import { SignIn } from '@/features/auth/sign-in'
import { parseApiError } from '@/lib/api/error'

const searchSchema = z.object({
  redirect: z.string().optional(),
})

export const Route = createFileRoute('/(auth)/sign-in')({
  validateSearch: searchSchema,
  beforeLoad: async ({ context }) => {
    try {
      const me = await context.queryClient.ensureQueryData(meQueryOptions())
      const recentSlug = localStorage.getItem(LAST_WORKSPACE_SLUG_KEY)
      throw redirect({
        href: chooseWorkspaceEntry(me, recentSlug ?? undefined),
        replace: true,
      })
    } catch (error) {
      if (parseApiError(error).status !== 401) throw error
    }
    const status = await context.queryClient.ensureQueryData(
      bootstrapStatusQueryOptions()
    )
    if (!status.initialized) {
      throw redirect({ to: '/setup', replace: true })
    }
  },
  component: SignIn,
})
