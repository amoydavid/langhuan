import { Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  type AppLanguage,
  LANGUAGE_LABELS,
  SUPPORTED_LANGUAGES,
  setLanguage,
} from '@/lib/i18n'

export function LanguageForm() {
  const { t, i18n } = useTranslation()
  const current =
    i18n.language.startsWith('zh') ||
    !SUPPORTED_LANGUAGES.includes(i18n.language as AppLanguage)
      ? 'zh'
      : (i18n.language as AppLanguage)

  function onLanguageChange(lng: AppLanguage) {
    setLanguage(lng)
    toast.success(t('settings.language.saved'))
  }

  return (
    <div className='space-y-6'>
      <div className='mb-3'>
        <Label>{t('settings.language.label')}</Label>
        <p className='mt-1 text-muted-foreground text-sm'>
          {t('settings.language.description')}
        </p>
      </div>
      <RadioGroup
        value={current}
        onValueChange={(value) => onLanguageChange(value as AppLanguage)}
        className='grid gap-3 sm:grid-cols-2'
      >
        {SUPPORTED_LANGUAGES.map((lng) => (
          <Label key={lng} className='group cursor-pointer font-normal'>
            <RadioGroupItem
              value={lng}
              aria-label={LANGUAGE_LABELS[lng]}
              className='sr-only'
            />
            <div className='flex items-start gap-2 rounded-[10px] border border-border bg-card p-3 transition-colors group-hover:border-border-strong group-has-data-[state=checked]:border-primary group-has-data-[state=checked]:ring-2 group-has-data-[state=checked]:ring-primary/15'>
              <Languages className='mt-0.5 size-4 text-primary' />
              <div className='font-medium text-sm'>{LANGUAGE_LABELS[lng]}</div>
            </div>
          </Label>
        ))}
      </RadioGroup>
    </div>
  )
}
