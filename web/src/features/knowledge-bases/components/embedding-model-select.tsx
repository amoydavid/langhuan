import { Boxes, Building2 } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import type { Role } from '@/features/auth/types'
import type { Model } from '@/features/models/types'

type EmbeddingModelSelectProps = {
  workspaceSlug: string
  workspaceRole: Role
  models: Model[]
  value: string
  onChange: (modelId: string) => void
  disabled?: boolean
  error?: string
}

export function EmbeddingModelSelect({
  workspaceSlug,
  workspaceRole,
  models,
  value,
  onChange,
  disabled,
  error,
}: EmbeddingModelSelectProps) {
  const { t } = useTranslation()
  const workspaceModels = models.filter(
    (model) => model.provider.scope === 'workspace'
  )
  const platformModels = models.filter(
    (model) => model.provider.scope === 'platform'
  )
  const canManage = workspaceRole === 'owner' || workspaceRole === 'admin'

  useEffect(() => {
    if (!value && models.length === 1) onChange(models[0]?.id ?? '')
  }, [models, onChange, value])

  return (
    <div className='grid gap-2'>
      <Label htmlFor='embedding-model'>
        {t('knowledgeBases.embeddingModelSelect.label')}
      </Label>
      <select
        id='embedding-model'
        className='h-10 w-full rounded-md border bg-background px-3 text-sm disabled:cursor-not-allowed disabled:opacity-60'
        value={value}
        disabled={disabled || models.length === 0}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={error ? true : undefined}
      >
        <option value=''>
          {t('knowledgeBases.embeddingModelSelect.placeholder')}
        </option>
        {workspaceModels.length > 0 && (
          <optgroup
            label={t('knowledgeBases.embeddingModelSelect.workspaceGroup')}
          >
            {workspaceModels.map((model) => (
              <ModelOption key={model.id} model={model} />
            ))}
          </optgroup>
        )}
        {platformModels.length > 0 && (
          <optgroup
            label={t('knowledgeBases.embeddingModelSelect.platformGroup')}
          >
            {platformModels.map((model) => (
              <ModelOption key={model.id} model={model} />
            ))}
          </optgroup>
        )}
      </select>
      {models.length > 0 && (
        <div className='flex flex-wrap gap-x-4 gap-y-1 text-muted-foreground text-xs'>
          {workspaceModels.length > 0 && (
            <span className='inline-flex items-center gap-1.5'>
              <Boxes className='size-3.5' />
              {t('knowledgeBases.embeddingModelSelect.workspaceGroup')}
            </span>
          )}
          {platformModels.length > 0 && (
            <span className='inline-flex items-center gap-1.5'>
              <Building2 className='size-3.5' />
              {t('knowledgeBases.embeddingModelSelect.platformGroup')}
            </span>
          )}
        </div>
      )}
      {models.length === 0 && (
        <div className='rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-sm'>
          {canManage ? (
            <p>
              {t('knowledgeBases.embeddingModelSelect.noModelsManageable')}{' '}
              <a
                href={`/workspaces/${encodeURIComponent(workspaceSlug)}/models`}
                className='font-medium text-primary hover:underline'
              >
                {t('knowledgeBases.embeddingModelSelect.configureModelsLink')}
              </a>
            </p>
          ) : (
            <p>{t('knowledgeBases.embeddingModelSelect.noModelsMember')}</p>
          )}
        </div>
      )}
      {error && <p className='text-destructive text-sm'>{error}</p>}
    </div>
  )
}

function ModelOption({ model }: { model: Model }) {
  const { t } = useTranslation()
  return (
    <option value={model.id}>
      {t('knowledgeBases.embeddingModelSelect.option', {
        name: model.display_name,
        provider: model.provider.display_name,
        dimensions: model.dimensions,
      })}
    </option>
  )
}
