import {
  CheckCircle2,
  CircleOff,
  History,
  Pencil,
  Scissors,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SafeMarkdown } from '@/components/safe-markdown'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  anchorLabel,
  revisionStatusLabel,
} from '@/features/chunks/inspector/chunk-inspector'
import type { Chunk, ChunkRevision } from '@/features/chunks/types'
import type { DocumentKind } from '@/features/documents/types'

type ChunkDetailDialogProps = {
  chunk?: Chunk
  documentTitle: string
  documentKind: DocumentKind
  revisions?: ChunkRevision[]
  canEdit: boolean
  onOpenChange: (open: boolean) => void
  onEdit?: (chunk: Chunk) => void
}

export function ChunkDetailDialog({
  chunk,
  documentTitle,
  documentKind,
  revisions = [],
  canEdit,
  onOpenChange,
  onEdit,
}: ChunkDetailDialogProps) {
  const { t } = useTranslation()
  const open = Boolean(chunk)
  const revision = chunk?.active_revision

  const editable =
    open && canEdit && documentKind !== 'faq' && Boolean(revision)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90svh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>
            {chunk
              ? t('chunks.inspector.title', {
                  sequence: chunk.sequence,
                  title: documentTitle,
                })
              : t('chunks.inspector.detail.ariaLabel')}
          </DialogTitle>
          <DialogDescription>
            {chunk ? anchorLabel(chunk.source_anchor) : ''}
          </DialogDescription>
        </DialogHeader>

        {chunk && (
          <div className='space-y-4'>
            {editable && (
              <div className='flex justify-end'>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => onEdit?.(chunk)}
                >
                  <Pencil />
                  {t('chunks.inspector.detail.editButton')}
                </Button>
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
                      <StatusBadge
                        tone={revision.enabled ? 'success' : 'neutral'}
                      >
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
                      <p className='text-muted-foreground text-xs'>
                        {t('chunks.inspector.detail.contentLabel')}
                      </p>
                      <p className='mt-1 font-medium text-sm'>
                        {revision.context_header ||
                          t('chunks.inspector.noContextHeader')}
                      </p>
                    </div>
                    <SafeMarkdown
                      content={
                        revision.content || t('chunks.inspector.emptyContent')
                      }
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
                  {anchorLabel(chunk.source_anchor)}
                </div>
                <pre className='whitespace-pre-wrap font-mono text-sm leading-6'>
                  {chunk.source_content}
                </pre>
              </TabsContent>

              <TabsContent
                value='history'
                className='rounded-xl border bg-card p-4'
              >
                {revisions.length === 0 ? (
                  <p className='text-muted-foreground text-sm'>
                    {t('chunks.inspector.historyEmpty')}
                  </p>
                ) : (
                  <ol className='divide-y divide-border'>
                    {[...revisions]
                      .sort(
                        (left, right) => right.revision_no - left.revision_no
                      )
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
                            tone={
                              item.status === 'failed' ? 'danger' : 'neutral'
                            }
                          >
                            {revisionStatusLabel(t)[item.status]}
                          </StatusBadge>
                        </li>
                      ))}
                  </ol>
                )}
              </TabsContent>
            </Tabs>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
