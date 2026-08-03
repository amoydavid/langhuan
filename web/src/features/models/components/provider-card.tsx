import { ArrowRight, Building2, KeyRound, RadioTower } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import type { ModelProvider, ModelScope } from '../types'

type ProviderCardProps = {
  provider: ModelProvider
  routeScope: ModelScope
  workspaceSlug?: string
}

export function ProviderCard({
  provider,
  routeScope,
  workspaceSlug,
}: ProviderCardProps) {
  const { t } = useTranslation()
  const providerLabels: Record<ModelProvider['provider'], string> = {
    openai: t('models.common.providerOpenAICompatible'),
    ark: t('models.common.providerArk'),
    ollama: t('models.common.providerOllama'),
    dashscope: t('models.common.providerDashscope'),
    tencentcloud: t('models.common.providerTencentcloud'),
  }
  const href =
    routeScope === 'platform'
      ? `/admin/models/${provider.id}`
      : `/workspaces/${encodeURIComponent(workspaceSlug ?? '')}/models/${provider.id}`
  const shared = routeScope === 'workspace' && provider.scope === 'platform'

  return (
    <Card className='group resource-card'>
      <CardHeader className='gap-4'>
        <div className='flex items-start justify-between gap-4'>
          <div className='flex min-w-0 items-start gap-3'>
            <div className='icon-tile'>
              {shared ? (
                <Building2 className='size-5 text-muted-foreground' />
              ) : (
                <RadioTower className='size-5 text-primary' />
              )}
            </div>
            <div className='min-w-0'>
              <h3 className='truncate font-semibold text-base'>
                {provider.display_name}
              </h3>
              <p className='mt-1 font-mono text-muted-foreground text-xs'>
                {provider.name}
              </p>
            </div>
          </div>
          <div className='flex shrink-0 flex-wrap justify-end gap-2'>
            <StatusBadge
              tone={provider.status === 'active' ? 'success' : 'neutral'}
            >
              {provider.status === 'active'
                ? t('models.common.statusActive')
                : t('models.common.statusDisabled')}
            </StatusBadge>
            {shared && (
              <Badge variant='outline'>
                {t('models.providerCard.sharedBadge')}
              </Badge>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className='space-y-5'>
        <p className='min-h-10 text-muted-foreground text-sm leading-5'>
          {provider.description || t('models.providerCard.noDescription')}
        </p>
        <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-4 text-sm'>
          <div className='flex flex-wrap items-center gap-x-4 gap-y-2 text-muted-foreground'>
            <span>{providerLabels[provider.provider]}</span>
            <span className='flex items-center gap-1.5'>
              <KeyRound className='size-3.5' />
              {provider.provider === 'ollama'
                ? t('models.providerCard.noCredentials')
                : provider.credentials_configured
                  ? t('models.providerCard.credentialsConfigured')
                  : t('models.providerCard.missingCredentials')}
            </span>
          </div>
          <a
            href={href}
            className='inline-flex items-center gap-1.5 font-medium text-primary hover:underline'
            aria-label={t('models.providerCard.viewLinkAriaLabel')}
          >
            {t('models.providerCard.viewLink')}
            <ArrowRight className='size-4 transition-transform group-hover:translate-x-0.5' />
          </a>
        </div>
      </CardContent>
    </Card>
  )
}
