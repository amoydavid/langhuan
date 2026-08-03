import { createFileRoute, redirect } from '@tanstack/react-router'
import {
  chooseWorkspaceEntry,
  LAST_WORKSPACE_SLUG_KEY,
} from '@/features/auth/navigation'
import {
  bootstrapStatusQueryOptions,
  meQueryOptions,
} from '@/features/auth/queries'
import { parseApiError } from '@/lib/api/error'

export const Route = createFileRoute('/')({
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
    throw redirect({
      to: status.initialized ? '/sign-in' : '/setup',
      replace: true,
    })
  },
})
