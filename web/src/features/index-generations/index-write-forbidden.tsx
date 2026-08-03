import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export function IndexWriteForbidden({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation()
  return (
    <div
      role='alert'
      className='mx-auto flex min-h-80 max-w-xl flex-col items-center justify-center rounded-xl border bg-card p-8 text-center'
    >
      <p className='font-semibold text-destructive text-sm'>403</p>
      <h2 className='mt-2 font-semibold text-2xl'>
        {t('indexGenerations.indexWriteForbidden.title')}
      </h2>
      <p className='mt-2 text-muted-foreground text-sm'>
        {t('indexGenerations.indexWriteForbidden.description')}
      </p>
      <Button type='button' variant='outline' className='mt-6' onClick={onBack}>
        <ArrowLeft />
        {t('indexGenerations.indexWriteForbidden.backButton')}
      </Button>
    </div>
  )
}
