import {
  Activity,
  Braces,
  FlaskConical,
  Pencil,
  Power,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import type { ConnectionTestResult, Model } from '../types'

type ModelCardProps = {
  model: Model
  canManage: boolean
  busy?: boolean
  onEdit?: () => void
  onTest?: () => void
  onToggle?: () => void
  onDelete?: () => void
  testResult?: ConnectionTestResult
}

export function ModelCard({
  model,
  canManage,
  busy,
  onEdit,
  onTest,
  onToggle,
  onDelete,
  testResult,
}: ModelCardProps) {
  const { t } = useTranslation()
  return (
    <Card className={model.status === 'disabled' ? 'bg-muted/20' : undefined}>
      <CardContent className='space-y-5'>
        <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-start'>
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <h3 className='font-semibold'>{model.display_name}</h3>
              <StatusBadge
                tone={model.status === 'active' ? 'success' : 'neutral'}
              >
                {model.status === 'active'
                  ? t('models.common.statusActive')
                  : t('models.common.statusDisabled')}
              </StatusBadge>
            </div>
            <p className='mt-1 break-all font-mono text-muted-foreground text-xs'>
              {model.model_name}
            </p>
          </div>
          <div className='flex flex-wrap items-center gap-2 text-muted-foreground text-xs'>
            {model.type === 'embedding' && model.dimensions ? (
              <span className='inline-flex items-center gap-1.5 rounded-md border px-2 py-1'>
                <Braces className='size-3.5' />
                {t('models.modelCard.dimensions', {
                  count: model.dimensions,
                })}
              </span>
            ) : (
              <span className='inline-flex items-center gap-1.5 rounded-md border px-2 py-1'>
                {t('models.common.modelTypeRerank')}
              </span>
            )}
            <span className='inline-flex items-center gap-1.5 rounded-md border px-2 py-1'>
              <Activity className='size-3.5' />
              {t('models.modelCard.referenceCount', {
                count: model.reference_count,
              })}
            </span>
          </div>
        </div>
        {model.description && (
          <p className='text-muted-foreground text-sm'>{model.description}</p>
        )}
        {testResult && (
          <div
            className='rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 text-sm'
            aria-live='polite'
          >
            <span className='font-medium text-emerald-700 dark:text-emerald-400'>
              连接测试通过
            </span>
            <span className='ml-2 text-muted-foreground'>
              {model.type === 'embedding'
                ? `${testResult.dimensions} 维 · ${testResult.duration_ms} ms`
                : `${testResult.result_count} 条结果 · ${testResult.duration_ms} ms`}
            </span>
          </div>
        )}
        {canManage && (
          <div className='flex flex-wrap gap-2 border-t pt-4'>
            <Button
              size='sm'
              variant='outline'
              disabled={busy}
              onClick={onTest}
            >
              <FlaskConical />
              {t('models.modelCard.testButton')}
            </Button>
            <Button
              size='sm'
              variant='outline'
              disabled={busy}
              onClick={onEdit}
            >
              <Pencil />
              {t('models.modelCard.editButton')}
            </Button>
            <Button
              size='sm'
              variant='outline'
              disabled={busy}
              onClick={onToggle}
            >
              <Power />
              {model.status === 'active'
                ? t('models.modelCard.disableButton')
                : t('models.modelCard.enableButton')}
            </Button>
            <Button
              size='sm'
              variant='outline'
              className='text-destructive'
              disabled={busy}
              onClick={onDelete}
            >
              <Trash2 />
              {t('models.modelCard.deleteButton')}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
