import { FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SafeMarkdown } from '@/components/safe-markdown'
import { StatusBadge } from '@/components/status-badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DocumentStatusBadge } from '@/features/documents/document-list'
import type { Document } from '@/features/documents/types'
import { formatDateTime } from '@/lib/i18n/datetime'

type DocumentPreviewProps = {
  document: Document
  displayName?: string
  path?: string
  initialView?: 'preview' | 'raw' | 'info'
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / (1024 * 1024)).toFixed(1)} MB`
}

function formatDate(value: string) {
  return formatDateTime(value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

export function DocumentPreview({
  document: item,
  displayName,
  path,
  initialView = 'preview',
}: DocumentPreviewProps) {
  const { t } = useTranslation()
  const name = displayName || item.title || t('content.documentPreview.unnamed')
  const revision = item.active_revision
  return (
    <article className='min-w-0 space-y-4'>
      <header className='flex flex-col justify-between gap-3 border-b pb-4 sm:flex-row sm:items-start'>
        <div className='min-w-0'>
          <div className='flex items-center gap-2'>
            <FileText className='size-4 shrink-0 text-primary' />
            <h2 className='truncate font-semibold text-lg'>{name}</h2>
            <DocumentStatusBadge status={item.status} />
          </div>
          {path && (
            <p className='mt-1 truncate text-muted-foreground text-xs'>
              {path}
            </p>
          )}
        </div>
        {revision && (
          <StatusBadge
            tone={revision.status === 'ready' ? 'success' : 'warning'}
          >
            Revision {revision.revision_no}
          </StatusBadge>
        )}
      </header>

      {item.error_message && (
        <div
          className='rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-destructive text-sm'
          role='alert'
        >
          {item.error_message}
        </div>
      )}

      <Tabs defaultValue={initialView}>
        <TabsList aria-label={t('content.documentPreview.tabListAriaLabel')}>
          <TabsTrigger value='preview'>
            {t('content.documentPreview.tabPreview')}
          </TabsTrigger>
          <TabsTrigger value='raw'>
            {t('content.documentPreview.tabRaw')}
          </TabsTrigger>
          <TabsTrigger value='info'>
            {t('content.documentPreview.tabInfo')}
          </TabsTrigger>
        </TabsList>
        <TabsContent
          value='preview'
          className='min-h-64 rounded-xl border bg-card p-5'
        >
          {item.normalized_markdown ? (
            <SafeMarkdown content={item.normalized_markdown} />
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t('content.documentPreview.noNormalizedContent')}
            </p>
          )}
        </TabsContent>
        <TabsContent value='raw' className='min-h-64'>
          <pre className='max-h-[56rem] overflow-auto whitespace-pre-wrap rounded-xl border bg-muted/30 p-5 font-mono text-sm leading-6'>
            {item.normalized_markdown ||
              t('content.documentPreview.noRawMarkdown')}
          </pre>
        </TabsContent>
        <TabsContent value='info' className='rounded-xl border bg-card p-5'>
          <dl className='grid gap-5 text-sm sm:grid-cols-2'>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldOriginalFilename')}
              </dt>
              <dd className='mt-1'>{revision?.original_filename || name}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldFileType')}
              </dt>
              <dd className='mt-1'>
                {revision?.file_type || t('content.documentPreview.unknown')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldMime')}
              </dt>
              <dd className='mt-1'>
                {revision?.content_type || t('content.documentPreview.unknown')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldSize')}
              </dt>
              <dd className='mt-1'>{formatBytes(revision?.size_bytes ?? 0)}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldRevision')}
              </dt>
              <dd className='mt-1'>
                {revision
                  ? t('content.documentPreview.revisionNo', {
                      no: revision.revision_no,
                    })
                  : t('content.documentPreview.noRevision')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldUpdatedAt')}
              </dt>
              <dd className='mt-1'>{formatDate(item.updated_at)}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('content.documentPreview.fieldSource')}
              </dt>
              <dd className='mt-1'>
                {item.source_type || t('content.documentPreview.unknown')}
              </dd>
            </div>
            {item.kind === 'web' && item.source_uri && (
              <div className='sm:col-span-2'>
                <dt className='text-muted-foreground'>
                  {t('content.documentPreview.fieldSourceUri')}
                </dt>
                <dd className='mt-1 break-all'>{item.source_uri}</dd>
              </div>
            )}
          </dl>
        </TabsContent>
      </Tabs>
    </article>
  )
}
