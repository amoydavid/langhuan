import { Boxes, Braces, Building2, RadioTower } from 'lucide-react'
import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import type { Model, ModelScope } from '../types'

type ModelCatalogProps = {
  models: Model[]
  routeScope: ModelScope
  workspaceSlug?: string
  isPending?: boolean
}

function availabilityLabel(model: Model) {
  if (model.status === 'disabled') return '模型已停用'
  if (model.provider.status === 'disabled') return '连接已停用'
  return model.available ? '可用' : '不可用'
}

function providerHref(
  model: Model,
  routeScope: ModelScope,
  workspaceSlug?: string
) {
  return routeScope === 'platform'
    ? `/admin/models/${model.provider_id}`
    : `/workspaces/${encodeURIComponent(workspaceSlug ?? '')}/models/${model.provider_id}`
}

export function ModelCatalog({
  models,
  routeScope,
  workspaceSlug,
  isPending,
}: ModelCatalogProps) {
  if (isPending) {
    return (
      <div className='grid gap-3'>
        <span className='sr-only'>正在加载模型</span>
        {[0, 1, 2].map((item) => (
          <div key={item} className='h-20 animate-pulse rounded-xl bg-muted' />
        ))}
      </div>
    )
  }
  if (models.length === 0) {
    return (
      <div className='flex min-h-48 flex-col items-center justify-center rounded-xl border border-dashed p-8 text-center'>
        <Boxes className='mb-3 size-8 text-muted-foreground/60' />
        <p className='font-medium'>没有符合条件的模型</p>
        <p className='mt-1 text-muted-foreground text-sm'>
          调整筛选条件，或前往连接管理添加模型。
        </p>
      </div>
    )
  }
  return (
    <>
      <div className='hidden overflow-hidden rounded-xl border md:block'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-left text-muted-foreground'>
            <tr>
              <th className='px-4 py-3 font-medium'>模型</th>
              <th className='px-4 py-3 font-medium'>类型</th>
              <th className='px-4 py-3 font-medium'>连接</th>
              <th className='px-4 py-3 font-medium'>上游模型</th>
              <th className='px-4 py-3 font-medium'>状态</th>
            </tr>
          </thead>
          <tbody className='divide-y'>
            {models.map((model) => (
              <tr key={model.id} className='hover:bg-muted/20'>
                <td className='px-4 py-4'>
                  <p className='font-medium'>{model.display_name}</p>
                  <p className='mt-1 text-muted-foreground text-xs'>
                    {model.reference_count} 个引用
                  </p>
                </td>
                <td className='px-4 py-4'>
                  <Badge variant='secondary'>
                    {model.type === 'embedding' ? 'Embedding' : 'Rerank'}
                  </Badge>
                </td>
                <td className='px-4 py-4'>
                  <a
                    className='font-medium text-primary hover:underline'
                    href={providerHref(model, routeScope, workspaceSlug)}
                  >
                    {model.provider.display_name}
                  </a>
                  <p className='mt-1 flex items-center gap-1 text-muted-foreground text-xs'>
                    {model.provider.scope === 'platform' ? (
                      <Building2 className='size-3' />
                    ) : (
                      <RadioTower className='size-3' />
                    )}
                    {model.provider.scope === 'platform'
                      ? '平台共享'
                      : '工作区'}
                  </p>
                </td>
                <td className='max-w-64 truncate px-4 py-4 font-mono text-xs'>
                  {model.model_name}
                </td>
                <td className='px-4 py-4'>
                  <StatusBadge tone={model.available ? 'success' : 'neutral'}>
                    {availabilityLabel(model)}
                  </StatusBadge>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className='grid gap-3 md:hidden'>
        {models.map((model) => (
          <article key={model.id} className='space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <h3 className='truncate font-semibold'>{model.display_name}</h3>
                <p className='mt-1 truncate font-mono text-muted-foreground text-xs'>
                  {model.model_name}
                </p>
              </div>
              <StatusBadge tone={model.available ? 'success' : 'neutral'}>
                {availabilityLabel(model)}
              </StatusBadge>
            </div>
            <div className='flex flex-wrap gap-2'>
              <Badge variant='secondary'>
                {model.type === 'embedding' ? (
                  <>
                    <Braces /> Embedding · {model.dimensions}
                  </>
                ) : (
                  'Rerank'
                )}
              </Badge>
              <Badge variant='outline'>{model.reference_count} 个引用</Badge>
            </div>
            <a
              className='block border-t pt-3 font-medium text-primary text-sm'
              href={providerHref(model, routeScope, workspaceSlug)}
            >
              {model.provider.display_name} · 查看连接
            </a>
          </article>
        ))}
      </div>
    </>
  )
}
