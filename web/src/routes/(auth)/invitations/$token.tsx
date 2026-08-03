import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { AuthLayout } from '@/features/auth/auth-layout'
import { InvitationRegistrationForm } from '@/features/auth/components/invitation-registration-form'
import { publicInvitationQueryOptions } from '@/features/auth/queries'
import i18n from '@/lib/i18n'

export const Route = createFileRoute('/(auth)/invitations/$token')({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      publicInvitationQueryOptions(params.token)
    ),
  pendingComponent: () => (
    <InvitationState message={i18n.t('routes.auth.invitation.loading')} />
  ),
  errorComponent: () => (
    <InvitationState message={i18n.t('routes.auth.invitation.invalid')} />
  ),
  component: InvitationPage,
})

function InvitationPage() {
  const { t } = useTranslation()
  const invitation = Route.useLoaderData()
  const { token } = Route.useParams()
  return (
    <AuthLayout>
      <Card className='w-full max-w-md gap-5 border-border-strong/70 bg-card/95'>
        <CardHeader>
          <CardTitle className='text-xl tracking-tight'>
            {t('routes.auth.invitation.title')}
          </CardTitle>
          <CardDescription>
            {t('routes.auth.invitation.description', {
              name: invitation.workspace_name,
            })}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <InvitationRegistrationForm invitation={invitation} token={token} />
        </CardContent>
      </Card>
    </AuthLayout>
  )
}

function InvitationState({ message }: { message: string }) {
  return (
    <AuthLayout>
      <Card className='w-full max-w-sm border-border-strong/70 bg-card/95'>
        <CardContent className='py-8 text-center text-muted-foreground text-sm'>
          {message}
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
