import { AlertCircle, CheckCircle2, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { parseApiError } from '@/lib/api/error'
import { formatDateTime } from '@/lib/i18n/datetime'
import type { IndexGeneration } from './types'

type GenerationListProps = {
  generations: IndexGeneration[]
  activeGenerationId?: string
  currentContentVersion?: number
  canManage: boolean
  activateGeneration: (
    generation: IndexGeneration,
    archiveManualEdits: boolean
  ) => Promise<void>
}

function configNumber(config: Record<string, unknown>, key: string) {
  return typeof config[key] === 'number' ? config[key] : '—'
}

function statusVariant(status: IndexGeneration['status']) {
  if (status === 'failed') return 'destructive' as const
  if (status === 'ready') return 'outline' as const
  return 'secondary' as const
}

export function GenerationList({
  generations,
  activeGenerationId,
  currentContentVersion,
  canManage,
  activateGeneration,
}: GenerationListProps) {
  const { t } = useTranslation()
  const [candidate, setCandidate] = useState<IndexGeneration>()
  const [archiveConfirmed, setArchiveConfirmed] = useState(false)
  const [activating, setActivating] = useState(false)
  const [activationError, setActivationError] = useState<string>()

  const requiresArchive = candidate?.manual_edit_disposition === 'pending'

  const statusLabels: Record<IndexGeneration['status'], string> = {
    building: t('indexGenerations.generationList.status.building'),
    ready: t('indexGenerations.generationList.status.ready'),
    stale: t('indexGenerations.generationList.status.stale'),
    failed: t('indexGenerations.generationList.status.failed'),
    retired: t('indexGenerations.generationList.status.retired'),
  }

  function openCandidate(item: IndexGeneration) {
    setCandidate(item)
    setArchiveConfirmed(false)
    setActivationError(undefined)
  }

  async function confirmActivation() {
    if (!candidate || (requiresArchive && !archiveConfirmed)) return
    setActivating(true)
    setActivationError(undefined)
    try {
      await activateGeneration(candidate, requiresArchive && archiveConfirmed)
      setCandidate(undefined)
    } catch (error) {
      setActivationError(parseApiError(error).message)
    } finally {
      setActivating(false)
    }
  }

  return (
    <div className='space-y-4'>
      {!canManage && (
        <Alert>
          <AlertCircle />
          <AlertTitle>
            {t('indexGenerations.generationList.readOnlyAlert.title')}
          </AlertTitle>
          <AlertDescription>
            {t('indexGenerations.generationList.readOnlyAlert.description')}
          </AlertDescription>
        </Alert>
      )}

      {generations.length === 0 ? (
        <div className='rounded-xl border border-dashed p-8 text-center'>
          <p className='font-medium'>
            {t('indexGenerations.generationList.empty.title')}
          </p>
          <p className='mt-1 text-muted-foreground text-sm'>
            {t('indexGenerations.generationList.empty.description')}
          </p>
        </div>
      ) : (
        <div className='space-y-3'>
          {generations.map((item) => {
            const active = item.id === activeGenerationId
            const canActivate = canManage && !active && item.status === 'ready'
            return (
              <Card key={item.id}>
                <CardHeader>
                  <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-start'>
                    <div className='min-w-0 space-y-2'>
                      <div className='flex flex-wrap items-center gap-2'>
                        {active && (
                          <Badge>
                            <CheckCircle2 />
                            {t('indexGenerations.generationList.activeBadge')}
                          </Badge>
                        )}
                        {!active && (
                          <Badge variant={statusVariant(item.status)}>
                            {item.status === 'building' && (
                              <Loader2 className='animate-spin' />
                            )}
                            {statusLabels[item.status]}
                          </Badge>
                        )}
                        <CardTitle className='text-base'>
                          {item.model_name}
                        </CardTitle>
                      </div>
                      <p className='text-muted-foreground text-sm'>
                        {formatDateTime(item.created_at)} ·{' '}
                        {t('indexGenerations.generationList.dimensions', {
                          count: item.embedding_dimension,
                        })}{' '}
                        ·{' '}
                        {active
                          ? t(
                              'indexGenerations.generationList.contentVersion',
                              {
                                version:
                                  currentContentVersion ??
                                  item.indexed_content_version,
                              }
                            )
                          : t(
                              'indexGenerations.generationList.contentSnapshot',
                              {
                                version: item.source_content_version,
                              }
                            )}{' '}
                        ·{' '}
                        {t('indexGenerations.generationList.indexedVersion', {
                          version: item.indexed_content_version,
                        })}
                      </p>
                    </div>
                    {canActivate && (
                      <Button
                        variant='outline'
                        onClick={() => openCandidate(item)}
                      >
                        {t('indexGenerations.generationList.compareActivate')}
                      </Button>
                    )}
                  </div>
                </CardHeader>
                <CardContent className='space-y-3 text-sm'>
                  <div className='grid gap-2 rounded-lg bg-muted/30 p-3 sm:grid-cols-2 xl:grid-cols-4'>
                    <span>
                      {t('indexGenerations.generationList.config.chunk', {
                        size: configNumber(item.chunking_config, 'chunk_size'),
                        overlap: configNumber(
                          item.chunking_config,
                          'chunk_overlap'
                        ),
                      })}
                    </span>
                    <span>
                      Vector{' '}
                      {configNumber(item.retrieval_config, 'vector_top_k')}
                    </span>
                    <span>
                      Keyword{' '}
                      {configNumber(item.retrieval_config, 'keyword_top_k')}
                    </span>
                    <span>
                      Final {configNumber(item.retrieval_config, 'final_top_k')}
                    </span>
                    <span>
                      {t(
                        'indexGenerations.generationList.rerankManagedByWorkspace'
                      )}
                    </span>
                  </div>
                  <p className='text-muted-foreground'>
                    {t('indexGenerations.generationList.stats.documents', {
                      count: item.document_count,
                    })}{' '}
                    ·{' '}
                    {t('indexGenerations.generationList.stats.chunks', {
                      count: item.chunk_count,
                    })}{' '}
                    ·{' '}
                    {t('indexGenerations.generationList.stats.manualEdits', {
                      count: item.manual_edit_count,
                    })}{' '}
                    ·{' '}
                    {t('indexGenerations.generationList.stats.disabledChunks', {
                      count: item.disabled_chunk_count,
                    })}
                  </p>
                  {item.status === 'failed' && item.error_message && (
                    <p role='alert' className='text-destructive'>
                      {item.error_message}
                    </p>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      <Dialog
        open={!!candidate}
        onOpenChange={(open) => {
          if (!open && !activating) setCandidate(undefined)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('indexGenerations.generationList.dialog.title')}
            </DialogTitle>
            <DialogDescription>
              {t('indexGenerations.generationList.dialog.description')}
            </DialogDescription>
          </DialogHeader>
          {candidate && (
            <div className='space-y-4 text-sm'>
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='rounded-lg border p-3'>
                  <p className='text-muted-foreground text-xs'>
                    {t('indexGenerations.generationList.dialog.candidateModel')}
                  </p>
                  <p className='mt-1 font-medium'>{candidate.model_name}</p>
                  <p className='mt-1 text-muted-foreground'>
                    {t(
                      'indexGenerations.generationList.dialog.dimensionAndChunks',
                      {
                        dimension: candidate.embedding_dimension,
                        chunks: candidate.chunk_count,
                      }
                    )}
                  </p>
                </div>
                <div className='rounded-lg border p-3'>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'indexGenerations.generationList.dialog.contentSnapshot'
                    )}
                  </p>
                  <p className='mt-1 font-medium'>
                    {t(
                      'indexGenerations.generationList.dialog.snapshotVersion',
                      {
                        version: candidate.source_content_version,
                      }
                    )}
                  </p>
                  <p className='mt-1 text-muted-foreground'>
                    {t(
                      'indexGenerations.generationList.dialog.snapshotIndexed',
                      {
                        version: candidate.indexed_content_version,
                      }
                    )}
                  </p>
                </div>
              </div>
              {requiresArchive && (
                <div className='rounded-lg border border-amber-500/30 bg-amber-500/5 p-3'>
                  <p className='font-medium'>
                    {t('indexGenerations.generationList.dialog.archiveCount', {
                      count: candidate.manual_edit_count,
                    })}
                  </p>
                  <p className='mt-1 text-muted-foreground text-xs'>
                    {t(
                      'indexGenerations.generationList.dialog.archiveDescription'
                    )}
                  </p>
                  <div className='mt-3 flex items-center gap-2'>
                    <Checkbox
                      id='archive-manual-edits'
                      checked={archiveConfirmed}
                      onCheckedChange={(checked) =>
                        setArchiveConfirmed(checked === true)
                      }
                    />
                    <Label htmlFor='archive-manual-edits'>
                      {t(
                        'indexGenerations.generationList.dialog.archiveConfirmLabel'
                      )}
                    </Label>
                  </div>
                </div>
              )}
              {activationError && (
                <p role='alert' className='text-destructive'>
                  {activationError}
                </p>
              )}
            </div>
          )}
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setCandidate(undefined)}
              disabled={activating}
            >
              {t('indexGenerations.generationList.dialog.cancel')}
            </Button>
            <Button
              onClick={() => void confirmActivation()}
              disabled={activating || (requiresArchive && !archiveConfirmed)}
            >
              {activating && <Loader2 className='animate-spin' />}
              {t('indexGenerations.generationList.dialog.confirmActivate')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
