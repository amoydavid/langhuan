import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AppearanceControls } from '@/components/appearance-controls'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
}

export function GeneralError({
  className,
  minimal = false,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  return (
    <div className={cn('relative h-svh w-full', className)}>
      {!minimal && <AppearanceControls />}
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        {!minimal && (
          <h1 className='font-bold text-[7rem] leading-tight'>500</h1>
        )}
        <span className='font-medium'>{t('routes.errors.general.title')}</span>
        <p className='text-center text-muted-foreground'>
          {t('routes.errors.general.description')}
        </p>
        {!minimal && (
          <div className='mt-6 flex gap-4'>
            <Button variant='outline' onClick={() => history.go(-1)}>
              {t('routes.errors.back')}
            </Button>
            <Button onClick={() => navigate({ to: '/' })}>
              {t('routes.errors.home')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
