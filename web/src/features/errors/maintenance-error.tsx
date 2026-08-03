import { useTranslation } from 'react-i18next'
import { AppearanceControls } from '@/components/appearance-controls'
import { Button } from '@/components/ui/button'

export function MaintenanceError() {
  const { t } = useTranslation()
  return (
    <div className='relative h-svh'>
      <AppearanceControls />
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        <h1 className='font-bold text-[7rem] leading-tight'>503</h1>
        <span className='font-medium'>
          {t('routes.errors.maintenance.title')}
        </span>
        <p className='text-center text-muted-foreground'>
          {t('routes.errors.maintenance.description')}
        </p>
        <div className='mt-6 flex gap-4'>
          <Button variant='outline' onClick={() => window.location.reload()}>
            {t('routes.errors.maintenance.reload')}
          </Button>
        </div>
      </div>
    </div>
  )
}
