import { useQuery } from '@tanstack/react-query'
import { Plus, RadioTower } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ProviderCard } from './components/provider-card'
import { ProviderForm } from './components/provider-form'
import { modelProvidersQueryOptions } from './queries'
import type { ModelProvider, ModelScope } from './types'

type ModelProviderListPageProps = {
  scope: ModelScope
  workspaceSlug?: string
  canManage: boolean
}

export function ModelProviderListPage({
  scope,
  workspaceSlug,
  canManage,
}: ModelProviderListPageProps) {
  const { t } = useTranslation()
  const [createOpen, setCreateOpen] = useState(false)
  const { data: providers = [], isPending } = useQuery(
    modelProvidersQueryOptions(scope, workspaceSlug)
  )
  const own = providers.filter((provider) => provider.scope === scope)
  const shared = providers.filter(
    (provider) => scope === 'workspace' && provider.scope === 'platform'
  )

  return (
    <div className='space-y-8'>
      <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
        <div>
          <p className='page-eyebrow'>
            {scope === 'platform'
              ? t('models.listPage.platformEyebrow')
              : t('models.listPage.workspaceEyebrow')}
          </p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('models.listPage.title')}
          </h1>
          <p className='mt-2 max-w-2xl text-muted-foreground'>
            {scope === 'platform'
              ? t('models.listPage.platformDescription')
              : t('models.listPage.workspaceDescription')}
          </p>
        </div>
        {canManage && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus />
            {t('models.listPage.createButton')}
          </Button>
        )}
      </div>

      {isPending ? (
        <div className='rounded-xl border border-dashed p-12 text-center text-muted-foreground text-sm'>
          {t('models.listPage.loading')}
        </div>
      ) : (
        <>
          <ProviderSection
            title={
              scope === 'platform'
                ? t('models.listPage.platformSectionTitle')
                : t('models.listPage.workspaceSectionTitle')
            }
            description={
              scope === 'platform'
                ? t('models.listPage.platformSectionDescription')
                : t('models.listPage.workspaceSectionDescription')
            }
            providers={own}
            routeScope={scope}
            workspaceSlug={workspaceSlug}
          />
          {scope === 'workspace' && (
            <ProviderSection
              title={t('models.listPage.sharedSectionTitle')}
              description={t('models.listPage.sharedSectionDescription')}
              providers={shared}
              routeScope={scope}
              workspaceSlug={workspaceSlug}
            />
          )}
        </>
      )}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>{t('models.listPage.createDialogTitle')}</DialogTitle>
            <DialogDescription>
              {t('models.listPage.createDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          <ProviderForm
            scope={scope}
            workspaceSlug={workspaceSlug}
            onSaved={() => setCreateOpen(false)}
          />
        </DialogContent>
      </Dialog>
    </div>
  )
}

type ProviderSectionProps = {
  title: string
  description: string
  providers: ModelProvider[]
  routeScope: ModelScope
  workspaceSlug?: string
}

function ProviderSection({
  title,
  description,
  providers,
  routeScope,
  workspaceSlug,
}: ProviderSectionProps) {
  const { t } = useTranslation()
  return (
    <section className='space-y-4' aria-labelledby={`section-${title}`}>
      <div>
        <h2 id={`section-${title}`} className='font-semibold text-lg'>
          {title}
        </h2>
        <p className='mt-1 text-muted-foreground text-sm'>{description}</p>
      </div>
      {providers.length === 0 ? (
        <div className='flex min-h-36 flex-col items-center justify-center rounded-xl border border-dashed bg-muted/10 p-8 text-center'>
          <RadioTower className='mb-3 size-7 text-muted-foreground/60' />
          <p className='font-medium text-sm'>
            {t('models.listPage.emptyTitle')}
          </p>
          <p className='mt-1 text-muted-foreground text-xs'>
            {t('models.listPage.emptyDescription')}
          </p>
        </div>
      ) : (
        <div className='grid gap-4 xl:grid-cols-2'>
          {providers.map((provider) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              routeScope={routeScope}
              workspaceSlug={workspaceSlug}
            />
          ))}
        </div>
      )}
    </section>
  )
}
