import { Check, Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  type AppLanguage,
  LANGUAGE_LABELS,
  SUPPORTED_LANGUAGES,
  setLanguage,
} from '@/lib/i18n'
import { cn } from '@/lib/utils'

export function LanguageSwitch() {
  const { t, i18n } = useTranslation()
  const current = i18n.language.startsWith('zh')
    ? 'zh'
    : ((SUPPORTED_LANGUAGES.includes(i18n.language as AppLanguage)
        ? i18n.language
        : 'en') as AppLanguage)

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant='ghost'
          size='icon'
          className='scale-95 rounded-full'
          aria-label={t('common.languageSwitchAriaLabel')}
        >
          <Languages className='size-[1.2rem]' />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {SUPPORTED_LANGUAGES.map((lng) => (
          <DropdownMenuItem key={lng} onClick={() => setLanguage(lng)}>
            {LANGUAGE_LABELS[lng]}
            <Check
              size={14}
              className={cn('ms-auto', current !== lng && 'hidden')}
            />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
