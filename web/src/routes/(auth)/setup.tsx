import { createFileRoute, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { AuthLayout } from '@/features/auth/auth-layout'
import { SetupForm } from '@/features/auth/components/setup-form'
import { bootstrapStatusQueryOptions } from '@/features/auth/queries'

export const Route = createFileRoute('/(auth)/setup')({
  beforeLoad: async ({ context }) => {
    const status = await context.queryClient.ensureQueryData(
      bootstrapStatusQueryOptions()
    )
    if (status.initialized) {
      throw redirect({ to: '/sign-in', replace: true })
    }
  },
  component: SetupPage,
})

function SetupPage() {
  const { t } = useTranslation()
  return (
    <AuthLayout>
      <Card className='w-full max-w-2xl gap-5 border-border-strong/70 bg-card/95'>
        <CardHeader>
          <CardTitle className='text-xl tracking-tight'>
            {t('routes.auth.setup.title')}
          </CardTitle>
          <CardDescription>
            {t('routes.auth.setup.description')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <SetupForm />
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
