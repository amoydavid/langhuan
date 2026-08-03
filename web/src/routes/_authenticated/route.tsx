import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import { safeRedirect } from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'
import { parseApiError } from '@/lib/api/error'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions())
    } catch (error) {
      if (parseApiError(error).status !== 401) throw error
      throw redirect({
        to: '/sign-in',
        search: { redirect: safeRedirect(location.href) },
        replace: true,
      })
    }
  },
  component: AuthenticatedLayout,
})
