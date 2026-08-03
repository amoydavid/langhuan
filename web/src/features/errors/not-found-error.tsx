import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AppearanceControls } from '@/components/appearance-controls'
import { Button } from '@/components/ui/button'

export function NotFoundError() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  return (
    <div className='relative h-svh'>
      <AppearanceControls />
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        <h1 className='font-bold text-[7rem] leading-tight'>404</h1>
        <span className='font-medium'>{t('routes.errors.notFound.title')}</span>
        <p className='text-center text-muted-foreground'>
          {t('routes.errors.notFound.description')}
        </p>
        <div className='mt-6 flex gap-4'>
          <Button variant='outline' onClick={() => history.go(-1)}>
            {t('routes.errors.back')}
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>
            {t('routes.errors.home')}
          </Button>
        </div>
      </div>
    </div>
  )
}
