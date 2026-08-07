import { useQuery } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { bootstrapStatusQueryOptions } from '@/features/auth/queries'
import { AuthLayout } from '../auth-layout'
import { OIDCLoginButton } from './components/oidc-login-button'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { t } = useTranslation()
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { data: status } = useQuery(bootstrapStatusQueryOptions())
  const oidcEnabled = status?.oidc_enabled ?? false
  const passwordEnabled = status?.password_enabled ?? true

  return (
    <AuthLayout>
      <Card className='w-full max-w-sm gap-5 border-border-strong/70 bg-card/95'>
        <CardHeader>
          <CardTitle className='text-xl tracking-tight'>
            {t('auth.signIn.title')}
          </CardTitle>
          <CardDescription>{t('auth.signIn.description')}</CardDescription>
        </CardHeader>
        <CardContent className='grid gap-4'>
          {oidcEnabled && <OIDCLoginButton next={redirect} />}
          {oidcEnabled && passwordEnabled && (
            <div className='relative'>
              <div className='absolute inset-0 flex items-center border-border-strong/70'>
                <span className='w-full border-t' />
              </div>
              <div className='relative flex justify-center text-xs uppercase'>
                <span className='bg-card px-2 text-muted-foreground'>
                  {t('auth.signIn.or')}
                </span>
              </div>
            </div>
          )}
          {passwordEnabled && <UserAuthForm redirectTo={redirect} />}
          {oidcEnabled && !passwordEnabled && (
            <p className='text-center text-muted-foreground text-sm'>
              {t('auth.signIn.ssoRedirectHint')}
            </p>
          )}
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
