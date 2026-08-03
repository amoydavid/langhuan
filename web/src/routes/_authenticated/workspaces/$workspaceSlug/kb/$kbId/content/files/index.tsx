import { createFileRoute } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

function FileEmptyState() {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-80 flex-col items-center justify-center rounded-xl border border-dashed bg-muted/15 text-center'>
      <FileText className='mb-3 size-7 text-muted-foreground' />
      <h2 className='font-medium'>
        {t('routes.workspaces.kb.content.files.selectTitle')}
      </h2>
      <p className='mt-1 max-w-sm text-muted-foreground text-sm'>
        {t('routes.workspaces.kb.content.files.selectDescription')}
      </p>
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/'
)({
  component: FileEmptyState,
})
