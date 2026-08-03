import type { TFunction } from 'i18next'
import {
  CheckCircle2,
  CircleOff,
  History,
  Pencil,
  Scissors,
} from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { SafeMarkdown } from '@/components/safe-markdown'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { Chunk, ChunkRevision } from '@/features/chunks/types'
import type { DocumentKind } from '@/features/documents/types'
import i18n from '@/lib/i18n'

type ChunkInspectorProps = {
  documentTitle: string
  documentKind: DocumentKind
  chunks: Chunk[]
  revisions?: ChunkRevision[]
  selectedChunkId?: string
  canEdit: boolean
  onSelectChunk?: (chunkId: string) => void
  onEdit?: (chunk: Chunk) => void
}

function numberField(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function anchorLabel(anchor: Record<string, unknown>) {
  const lineStart = numberField(anchor.line_start)
  const lineEnd = numberField(anchor.line_end)
  if (lineStart !== undefined && lineEnd !== undefined) {
    return i18n.t('chunks.inspector.anchor.range', {
      start: lineStart,
      end: lineEnd,
    })
  }
  const rowStart = numberField(anchor.row_start)
  const rowEnd = numberField(anchor.row_end)
  const sheet = typeof anchor.sheet === 'string' ? anchor.sheet : undefined
  if (rowStart !== undefined && rowEnd !== undefined) {
    return sheet
      ? i18n.t('chunks.inspector.anchor.rangeWithSheet', {
          sheet,
          start: rowStart,
          end: rowEnd,
        })
      : i18n.t('chunks.inspector.anchor.range', {
          start: rowStart,
          end: rowEnd,
        })
  }
  return i18n.t('chunks.inspector.anchor.unknown')
}

function revisionStatusLabel(t: TFunction) {
  return {
    pending: t('chunks.inspector.revisionStatus.pending'),
    indexing: t('chunks.inspector.revisionStatus.indexing'),
    ready: t('chunks.inspector.revisionStatus.ready'),
    failed: t('chunks.inspector.revisionStatus.failed'),
  } as const
}

export function ChunkInspector({
  documentTitle,
  documentKind,
  chunks,
  revisions = [],
  selectedChunkId,
  canEdit,
  onSelectChunk,
  onEdit,
}: ChunkInspectorProps) {
  const { t } = useTranslation()
  const selected =
    chunks.find((item) => item.id === selectedChunkId) ?? chunks[0]
  const titleRef = useRef<HTMLHeadingElement>(null)

  useEffect(() => {
    if (selectedChunkId && selected?.id === selectedChunkId) {
      titleRef.current?.focus()
      titleRef.current?.scrollIntoView({ block: 'nearest' })
    }
  }, [selected?.id, selectedChunkId])

  if (!selected) {
    return (
      <div className='rounded-xl border border-dashed p-6 text-center text-muted-foreground text-sm'>
        {t('chunks.inspector.emptyState')}
      </div>
    )
  }

  const revision = selected.active_revision
  return (
    <section
      className='min-w-0 space-y-4'
      aria-label={t('chunks.inspector.ariaLabel')}
    >
      <div className='flex items-center justify-between gap-3 border-b pb-3'>
        <div>
          <h2
            ref={titleRef}
            tabIndex={-1}
            className='font-semibold text-base outline-none focus-visible:ring-2 focus-visible:ring-ring/50'
          >
            {t('chunks.inspector.title', {
              sequence: selected.sequence,
              title: documentTitle,
            })}
          </h2>
          <p className='mt-1 text-muted-foreground text-xs'>
            {anchorLabel(selected.source_anchor)}
          </p>
        </div>
        {canEdit && documentKind !== 'faq' && revision && (
          <Button
            variant='outline'
            size='sm'
            onClick={() => onEdit?.(selected)}
          >
            <Pencil />
            {t('chunks.inspector.editButton')}
          </Button>
        )}
      </div>

      <fieldset
        className='flex gap-2 overflow-x-auto pb-1'
        aria-label={t('chunks.inspector.chunkListAriaLabel')}
      >
        {chunks.map((item) => (
          <button
            key={item.id}
            type='button'
            className='shrink-0 rounded-md border px-3 py-2 text-left text-xs hover:bg-muted aria-pressed:border-primary aria-pressed:bg-primary/5'
            aria-pressed={item.id === selected.id}
            onClick={() => onSelectChunk?.(item.id)}
          >
            #{item.sequence}{' '}
            {item.active_revision?.context_header ||
              t('chunks.inspector.chunkTabNoTitle')}
          </button>
        ))}
      </fieldset>

      {documentKind === 'faq' && (
        <div className='rounded-lg bg-muted/50 p-3 text-muted-foreground text-sm'>
          {t('chunks.inspector.faqNotice')}
        </div>
      )}

      <Tabs defaultValue='current'>
        <TabsList aria-label={t('chunks.inspector.tabsAriaLabel')}>
          <TabsTrigger value='current'>
            {t('chunks.inspector.tabCurrent')}
          </TabsTrigger>
          <TabsTrigger value='source'>
            {t('chunks.inspector.tabSource')}
          </TabsTrigger>
          <TabsTrigger value='history'>
            {t('chunks.inspector.tabHistory')}
          </TabsTrigger>
        </TabsList>
        <TabsContent
          value='current'
          className='space-y-4 rounded-xl border bg-card p-4'
        >
          {revision ? (
            <>
              <div className='flex flex-wrap gap-2'>
                <StatusBadge tone={revision.enabled ? 'success' : 'neutral'}>
                  {revision.enabled ? (
                    <CheckCircle2 className='size-3' />
                  ) : (
                    <CircleOff className='size-3' />
                  )}
                  {revision.enabled
                    ? t('chunks.inspector.statusEnabled')
                    : t('chunks.inspector.statusDisabled')}
                </StatusBadge>
                <StatusBadge
                  tone={revision.status === 'failed' ? 'danger' : 'info'}
                >
                  {revisionStatusLabel(t)[revision.status]}
                </StatusBadge>
                {revision.edit_source === 'user' && (
                  <StatusBadge tone='warning'>
                    {t('chunks.inspector.statusUserEdited')}
                  </StatusBadge>
                )}
              </div>
              <div>
                <p className='text-muted-foreground text-xs'>Context header</p>
                <p className='mt-1 font-medium text-sm'>
                  {revision.context_header ||
                    t('chunks.inspector.noContextHeader')}
                </p>
              </div>
              <SafeMarkdown
                content={revision.content || t('chunks.inspector.emptyContent')}
              />
              <div className='flex flex-wrap items-center gap-3 border-t pt-3 text-muted-foreground text-xs'>
                <span className='inline-flex items-center gap-1'>
                  <History className='size-3' />
                  {t('chunks.inspector.revisionNo', {
                    number: revision.revision_no,
                  })}
                </span>
                <span>{revision.editor_display_name}</span>
              </div>
            </>
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t('chunks.inspector.noActiveRevision')}
            </p>
          )}
        </TabsContent>
        <TabsContent
          value='source'
          className='rounded-xl border bg-muted/20 p-4'
        >
          <div className='mb-3 flex items-center gap-2 text-muted-foreground text-xs'>
            <Scissors className='size-3' />
            {anchorLabel(selected.source_anchor)}
          </div>
          <pre className='whitespace-pre-wrap font-mono text-sm leading-6'>
            {selected.source_content}
          </pre>
        </TabsContent>
        <TabsContent value='history' className='rounded-xl border bg-card p-4'>
          {revisions.length === 0 ? (
            <p className='text-muted-foreground text-sm'>
              {t('chunks.inspector.historyEmpty')}
            </p>
          ) : (
            <ol className='divide-y divide-border'>
              {[...revisions]
                .sort((left, right) => right.revision_no - left.revision_no)
                .map((item) => (
                  <li
                    key={item.id}
                    className='flex flex-wrap items-center justify-between gap-2 py-3 first:pt-0 last:pb-0'
                  >
                    <div>
                      <p className='font-medium text-sm'>
                        {t('chunks.inspector.revisionNo', {
                          number: item.revision_no,
                        })}
                      </p>
                      <p className='mt-1 text-muted-foreground text-xs'>
                        {item.editor_display_name} ·{' '}
                        {item.edit_source === 'user'
                          ? t('chunks.inspector.editSourceUser')
                          : t('chunks.inspector.editSourceSystem')}
                      </p>
                    </div>
                    <StatusBadge
                      tone={item.status === 'failed' ? 'danger' : 'neutral'}
                    >
                      {revisionStatusLabel(t)[item.status]}
                    </StatusBadge>
                  </li>
                ))}
            </ol>
          )}
        </TabsContent>
      </Tabs>
    </section>
  )
}
