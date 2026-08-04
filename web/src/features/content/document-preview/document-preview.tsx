import { useQuery } from '@tanstack/react-query'
import { FileText, ImageIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SafeMarkdown } from '@/components/safe-markdown'
import { StatusBadge } from '@/components/status-badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getDocumentAssets } from '@/features/documents/api'
import { DocumentStatusBadge } from '@/features/documents/document-list'
import type { Document, DocumentAsset } from '@/features/documents/types'
import { formatDateTime } from '@/lib/i18n/datetime'

type DocumentPreviewProps = {
  document: Document
  displayName?: string
  path?: string
  initialView?: 'preview' | 'raw' | 'info'
  /** 可选：提供 workspaceSlug + documentId 后，info tab 展示该文档的图片资产 */
  workspaceSlug?: string
  documentId?: string
}

function documentAssetsQueryOptions(workspaceSlug: string, documentId: string) {
  return {
    queryKey: ['document-assets', workspaceSlug, documentId],
    queryFn: () => getDocumentAssets(workspaceSlug, documentId),
  }
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

function AssetGrid({
  assets,
  name,
}: {
  assets: DocumentAsset[]
  name: string
}) {
  return (
    <div className='grid gap-3 sm:grid-cols-2'>
      {assets.map((asset) => (
        <div
          key={asset.id}
          className='flex items-start gap-3 rounded-lg border bg-muted/20 p-3'
        >
          <div className='icon-tile size-10 shrink-0 items-center justify-center'>
            <ImageIcon className='size-4' />
          </div>
          <div className='min-w-0'>
            <p className='truncate font-medium text-sm'>{asset.original_ref}</p>
            <p className='mt-0.5 truncate text-muted-foreground text-xs'>
              {asset.mime_type} · {formatBytes(asset.size_bytes)} ·{' '}
              {asset.sha256.slice(0, 8)}…
            </p>
            {asset.public_url && (
              <a
                href={asset.public_url}
                target='_blank'
                rel='noreferrer'
                className='mt-1 inline-block truncate text-xs text-primary hover:underline'
              >
                {asset.public_url}
              </a>
            )}
          </div>
        </div>
      ))}
      {assets.length === 0 && (
        <p className='text-muted-foreground text-sm'>{name}</p>
      )}
    </div>
  )
}

export function DocumentPreview({
  document: item,
  displayName,
  path,
  initialView = 'preview',
  workspaceSlug,
  documentId,
}: DocumentPreviewProps) {
  const { t } = useTranslation()
  const name = displayName || item.title || t('content.documentPreview.unnamed')
  const revision = item.active_revision
  const showAssets = Boolean(workspaceSlug && documentId)
  const { data: assets = [] } = useQuery({
    ...documentAssetsQueryOptions(workspaceSlug ?? '', documentId ?? ''),
    enabled: showAssets,
  })
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

          {revision?.warnings && revision.warnings.length > 0 && (
            <div className='mt-6 border-t pt-5'>
              <h3 className='mb-3 text-sm font-semibold'>
                {t('content.documentPreview.warningsTitle', {
                  count: revision.warnings.length,
                })}
              </h3>
              <div className='space-y-2'>
                {revision.warnings.map((warning, index) => (
                  <div
                    key={`${warning.code}-${index}`}
                    className='rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-sm'
                  >
                    <span className='font-mono text-xs text-amber-700'>
                      {warning.code}
                    </span>
                    <span className='ml-2 text-muted-foreground'>
                      {warning.message}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {showAssets && (
            <div className='mt-6 border-t pt-5'>
              <h3 className='mb-3 flex items-center gap-2 text-sm font-semibold'>
                <ImageIcon className='size-4 text-primary' />
                {t('content.documentPreview.assetsTitle', {
                  count: assets.length,
                })}
              </h3>
              <AssetGrid
                assets={assets}
                name={t('content.documentPreview.noAssets')}
              />
            </div>
          )}
        </TabsContent>
      </Tabs>
    </article>
  )
}
