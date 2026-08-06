import { KeyRound, PlugZap } from 'lucide-react'
import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import type { ModelProvider, ModelScope } from '../types'

type ProviderCatalogProps = {
  providers: ModelProvider[]
  routeScope: ModelScope
  workspaceSlug?: string
  isPending?: boolean
}

function detailHref(
  provider: ModelProvider,
  routeScope: ModelScope,
  workspaceSlug?: string
) {
  return routeScope === 'platform'
    ? `/admin/models/${provider.id}`
    : `/workspaces/${encodeURIComponent(workspaceSlug ?? '')}/models/${provider.id}`
}

function capabilityLabel(capability: string) {
  if (capability === 'embedding') return 'Embedding'
  if (capability === 'rerank') return 'Rerank'
  return '文档解析'
}

export function ProviderCatalog({
  providers,
  routeScope,
  workspaceSlug,
  isPending,
}: ProviderCatalogProps) {
  if (isPending) {
    return <div className='h-52 animate-pulse rounded-xl bg-muted' />
  }
  if (providers.length === 0) {
    return (
      <div className='flex min-h-48 flex-col items-center justify-center rounded-xl border border-dashed p-8 text-center'>
        <PlugZap className='mb-3 size-8 text-muted-foreground/60' />
        <p className='font-medium'>没有符合条件的连接</p>
        <p className='mt-1 text-muted-foreground text-sm'>
          新建连接后，可以在同一连接下添加多个模型。
        </p>
      </div>
    )
  }
  return (
    <div className='grid gap-3'>
      {providers.map((provider) => {
        const parserOnly =
          provider.capabilities.length === 1 &&
          provider.capabilities[0] === 'parser'
        return (
          <a
            key={provider.id}
            href={detailHref(provider, routeScope, workspaceSlug)}
            className='group grid gap-4 rounded-xl border p-4 transition-colors hover:bg-muted/30 md:grid-cols-[minmax(0,1fr)_auto_auto_auto] md:items-center'
          >
            <div className='min-w-0'>
              <p className='truncate font-semibold'>{provider.display_name}</p>
              <p className='mt-1 text-muted-foreground text-sm'>
                {provider.provider === 'siliconflow'
                  ? '硅基流动 SiliconFlow'
                  : provider.provider}
              </p>
            </div>
            <div className='flex flex-wrap gap-2'>
              {provider.capabilities.map((capability) => (
                <Badge key={capability} variant='secondary'>
                  {capabilityLabel(capability)}
                </Badge>
              ))}
            </div>
            <div className='text-sm'>
              {parserOnly ? (
                <span className='text-muted-foreground'>模型不适用</span>
              ) : (
                <>
                  <span className='font-semibold'>
                    {provider.model_counts.total}
                  </span>
                  <span className='text-muted-foreground'> 个模型 · </span>
                  <span className='font-semibold'>
                    {provider.model_counts.active}
                  </span>
                  <span className='text-muted-foreground'> 可用</span>
                </>
              )}
            </div>
            <div className='flex items-center gap-3'>
              <span className='flex items-center gap-1 text-muted-foreground text-xs'>
                <KeyRound className='size-3.5' />
                {provider.credential_fields.length === 0
                  ? '无需凭证'
                  : provider.credentials_configured
                    ? '凭证已配置'
                    : '缺少凭证'}
              </span>
              <StatusBadge
                tone={provider.status === 'active' ? 'success' : 'neutral'}
              >
                {provider.status === 'active' ? '运行中' : '已停用'}
              </StatusBadge>
            </div>
          </a>
        )
      })}
    </div>
  )
}
