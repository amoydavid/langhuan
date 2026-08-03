import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { WorkspacePicker } from '@/features/workspaces/workspace-picker'

export const Route = createFileRoute('/_authenticated/workspaces/')({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(meQueryOptions()),
  staticData: { breadcrumb: { label: 'routes.workspaces.breadcrumb' } },
  component: () => (
    <Main>
      <WorkspacePicker />
    </Main>
  ),
})
