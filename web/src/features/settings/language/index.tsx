import { useTranslation } from 'react-i18next'
import { ContentSection } from '../components/content-section'
import { LanguageForm } from './language-form'

export function SettingsLanguage() {
  const { t } = useTranslation()
  return (
    <ContentSection
      title={t('settings.language.title')}
      desc={t('settings.language.description')}
    >
      <LanguageForm />
    </ContentSection>
  )
}
