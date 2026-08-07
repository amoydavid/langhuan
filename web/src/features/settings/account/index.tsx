import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { bootstrapStatusQueryOptions } from '@/features/auth/queries'
import { ContentSection } from '../components/content-section'
import { IdentitiesForm } from './identities-form'
import { PasswordForm } from './password-form'

export function SettingsAccount() {
  const { t } = useTranslation()
  const { data: status } = useQuery(bootstrapStatusQueryOptions())
  const passwordEnabled = status?.password_enabled ?? true
  const oidcEnabled = status?.oidc_enabled ?? false

  return (
    <div className='space-y-8'>
      {passwordEnabled && (
        <ContentSection
          title={t('settings.account.passwordTitle')}
          desc={t('settings.account.passwordDescription')}
        >
          <PasswordForm />
        </ContentSection>
      )}
      <ContentSection
        title={t('settings.account.ssoTitle')}
        desc={t('settings.account.ssoDescription')}
      >
        <IdentitiesForm oidcEnabled={oidcEnabled} />
      </ContentSection>
    </div>
  )
}
