import { ArrowLeft, Pencil, Plus, Power, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { parseApiError } from '@/lib/api/error'
import type {
  ConnectionTestResult,
  Model,
  ModelProvider,
  ModelScope,
} from '../types'
import { ModelCard } from './model-card'
import { ModelEditor } from './model-editor'
import { ProviderConnectionSettings } from './provider-connection-settings'
import { ProviderForm } from './provider-form'

type ModelProviderDetailContentProps = {
  provider: ModelProvider
  models: Model[]
  routeScope: ModelScope
  workspaceSlug?: string
  canManage: boolean
  busy?: boolean
  error?: Error | null
  testResults?: Record<string, ConnectionTestResult>
  testPendingModelId?: string
  onProviderStatusToggle?: () => void
  onProviderDelete?: () => void
  onModelStatusToggle?: (model: Model) => void
  onModelDelete?: (model: Model) => void
  onModelTest?: (model: Model) => void
}

export function ModelProviderDetailContent({
  provider,
  models,
  routeScope,
  workspaceSlug,
  canManage,
  busy,
  error,
  testResults,
  testPendingModelId,
  onProviderStatusToggle,
  onProviderDelete,
  onModelStatusToggle,
  onModelDelete,
  onModelTest,
}: ModelProviderDetailContentProps) {
  const { t } = useTranslation()
  const [editProviderOpen, setEditProviderOpen] = useState(false)
  const [createModelOpen, setCreateModelOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<Model>()
  const [deletingModel, setDeletingModel] = useState<Model>()
  const [deleteProviderOpen, setDeleteProviderOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<'models' | 'connection'>('models')
  const mutable = canManage && provider.scope === routeScope
  const backHref =
    routeScope === 'platform'
      ? '/admin/models'
      : `/workspaces/${encodeURIComponent(workspaceSlug ?? '')}/models`

  return (
    <div className='space-y-8'>
      <div className='space-y-5'>
        <a
          href={backHref}
          className='inline-flex items-center gap-1.5 text-muted-foreground text-sm hover:text-foreground'
        >
          <ArrowLeft className='size-4' />
          {t('models.detailPage.backLink')}
        </a>
        <div className='flex flex-col justify-between gap-4 lg:flex-row lg:items-start'>
          <div>
            <div className='flex flex-wrap items-center gap-2'>
              <h1 className='font-semibold text-2xl tracking-tight'>
                {provider.display_name}
              </h1>
              <StatusBadge
                tone={provider.status === 'active' ? 'success' : 'neutral'}
              >
                {provider.status === 'active'
                  ? t('models.common.statusActive')
                  : t('models.common.statusDisabled')}
              </StatusBadge>
              {!mutable && provider.scope === 'platform' && (
                <Badge variant='outline'>
                  {t('models.detailPage.sharedReadonlyBadge')}
                </Badge>
              )}
            </div>
            <p className='mt-2 font-mono text-muted-foreground text-xs'>
              {provider.name} · {provider.provider}
            </p>
          </div>
          {mutable && (
            <div className='flex flex-wrap gap-2'>
              <Button
                variant='outline'
                onClick={() => setEditProviderOpen(true)}
              >
                <Pencil />
                {t('models.detailPage.editProviderButton')}
              </Button>
              <Button
                variant='outline'
                disabled={busy}
                onClick={onProviderStatusToggle}
              >
                <Power />
                {provider.status === 'active'
                  ? t('models.detailPage.disableProviderButton')
                  : t('models.detailPage.enableProviderButton')}
              </Button>
              <Button
                variant='outline'
                className='text-destructive'
                disabled={busy}
                onClick={() => setDeleteProviderOpen(true)}
              >
                <Trash2 />
                {t('models.detailPage.deleteProviderButton')}
              </Button>
            </div>
          )}
        </div>
      </div>

      <div className='flex gap-1 border-b'>
        <button
          type='button'
          onClick={() => setActiveTab('models')}
          className={`border-b-2 px-4 py-3 font-medium text-sm ${activeTab === 'models' ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground'}`}
        >
          模型 ({models.length})
        </button>
        <button
          type='button'
          onClick={() => setActiveTab('connection')}
          className={`border-b-2 px-4 py-3 font-medium text-sm ${activeTab === 'connection' ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground'}`}
        >
          连接设置
        </button>
      </div>

      {activeTab === 'connection' && (
        <ProviderConnectionSettings provider={provider} />
      )}

      {activeTab === 'models' && (
        <section className='space-y-4' aria-labelledby='provider-models-title'>
          <div className='flex items-end justify-between gap-4'>
            <div>
              <h2 id='provider-models-title' className='font-semibold text-lg'>
                {t('models.detailPage.modelsTitle')}
              </h2>
              <p className='mt-1 text-muted-foreground text-sm'>
                {t('models.detailPage.modelsDescription')}
              </p>
            </div>
            {mutable &&
              provider.capabilities.some(
                (capability) =>
                  capability === 'embedding' || capability === 'rerank'
              ) && (
                <Button onClick={() => setCreateModelOpen(true)}>
                  <Plus />
                  {t('models.detailPage.addModelButton')}
                </Button>
              )}
          </div>
          {error && (
            <p
              className='rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-destructive text-sm'
              role='alert'
            >
              {parseApiError(error).message}
            </p>
          )}
          {models.length === 0 ? (
            <div className='rounded-xl border border-dashed p-10 text-center text-muted-foreground text-sm'>
              {t('models.detailPage.emptyModels')}
            </div>
          ) : (
            <div className='grid gap-4'>
              {models.map((model) => (
                <ModelCard
                  key={model.id}
                  model={model}
                  canManage={mutable}
                  busy={busy || testPendingModelId === model.id}
                  testResult={testResults?.[model.id]}
                  onEdit={() => setEditingModel(model)}
                  onTest={() => onModelTest?.(model)}
                  onToggle={() => onModelStatusToggle?.(model)}
                  onDelete={() => setDeletingModel(model)}
                />
              ))}
            </div>
          )}
        </section>
      )}

      <Dialog open={editProviderOpen} onOpenChange={setEditProviderOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>
              {t('models.detailPage.editProviderDialogTitle')}
            </DialogTitle>
            <DialogDescription>
              {t('models.detailPage.editProviderDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          <ProviderForm
            scope={routeScope}
            workspaceSlug={workspaceSlug}
            provider={provider}
            onSaved={() => setEditProviderOpen(false)}
          />
        </DialogContent>
      </Dialog>

      <Dialog open={createModelOpen} onOpenChange={setCreateModelOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>
              {t('models.detailPage.addModelDialogTitle')}
            </DialogTitle>
            <DialogDescription>
              {t('models.detailPage.addModelDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          <ModelEditor
            provider={provider}
            scope={routeScope}
            workspaceSlug={workspaceSlug}
            onSaved={() => setCreateModelOpen(false)}
          />
        </DialogContent>
      </Dialog>

      <Dialog
        open={editingModel !== undefined}
        onOpenChange={(open) => !open && setEditingModel(undefined)}
      >
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>
              {t('models.detailPage.editModelDialogTitle')}
            </DialogTitle>
            <DialogDescription>
              {t('models.detailPage.editModelDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          {editingModel && (
            <ModelEditor
              provider={provider}
              scope={routeScope}
              workspaceSlug={workspaceSlug}
              model={editingModel}
              onSaved={() => setEditingModel(undefined)}
            />
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteProviderOpen}
        onOpenChange={setDeleteProviderOpen}
        title={t('models.detailPage.deleteProviderDialogTitle')}
        desc={t('models.detailPage.deleteProviderDialogDescription')}
        confirmText={t('models.detailPage.confirmDelete')}
        cancelBtnText={t('models.detailPage.cancel')}
        destructive
        isLoading={busy}
        handleConfirm={() => onProviderDelete?.()}
      />

      <ConfirmDialog
        open={deletingModel !== undefined}
        onOpenChange={(open) => !open && setDeletingModel(undefined)}
        title={t('models.detailPage.deleteModelDialogTitle')}
        desc={t('models.detailPage.deleteModelDialogDescription')}
        confirmText={t('models.detailPage.confirmDelete')}
        cancelBtnText={t('models.detailPage.cancel')}
        destructive
        isLoading={busy}
        handleConfirm={() => deletingModel && onModelDelete?.(deletingModel)}
      />
    </div>
  )
}
