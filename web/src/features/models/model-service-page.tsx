import { useQuery } from '@tanstack/react-query'
import { Boxes, Plus, Search, Unplug } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ModelCatalog } from './components/model-catalog'
import { ProviderCatalog } from './components/provider-catalog'
import { ProviderForm } from './components/provider-form'
import { modelCatalogQueryOptions, modelProvidersQueryOptions } from './queries'
import type { ModelServiceSearch } from './search-params'
import type { ModelScope } from './types'

type ModelServicePageProps = {
  scope: ModelScope
  workspaceSlug?: string
  canManage: boolean
  search: ModelServiceSearch
  onSearchChange: (next: Partial<ModelServiceSearch>) => void
}

export function ModelServicePage({
  scope,
  workspaceSlug,
  canManage,
  search,
  onSearchChange,
}: ModelServicePageProps) {
  const [createOpen, setCreateOpen] = useState(false)
  const providersQuery = useQuery(
    modelProvidersQueryOptions(scope, workspaceSlug)
  )
  const modelsQuery = useQuery(
    modelCatalogQueryOptions(scope, workspaceSlug, {
      type: search.type,
      status: search.status,
      scope: search.scope,
      q: search.q,
    })
  )
  const providers = (providersQuery.data ?? []).filter((provider) => {
    if (
      search.capability !== 'all' &&
      !provider.capabilities.includes(search.capability)
    )
      return false
    if (search.status !== 'all' && provider.status !== search.status)
      return false
    if (search.scope !== 'all' && provider.scope !== search.scope) return false
    if (
      search.q &&
      !`${provider.display_name} ${provider.name} ${provider.provider}`
        .toLocaleLowerCase()
        .includes(search.q.toLocaleLowerCase())
    )
      return false
    return true
  })

  return (
    <div className='space-y-6'>
      <header className='flex flex-col justify-between gap-4 lg:flex-row lg:items-end'>
        <div>
          <p className='page-eyebrow'>MODEL INFRASTRUCTURE</p>
          <h1 className='font-semibold text-2xl tracking-tight'>模型服务</h1>
          <p className='mt-2 max-w-2xl text-muted-foreground'>
            先管理可用模型；连接负责共享 API 地址、凭证、状态与能力。
          </p>
        </div>
        {canManage && (
          <Button
            onClick={() => {
              if (search.view === 'connections') setCreateOpen(true)
              else onSearchChange({ view: 'connections' })
            }}
          >
            <Plus />
            {search.view === 'connections' ? '新建连接' : '添加模型'}
          </Button>
        )}
      </header>

      <div className='flex flex-col gap-4 rounded-xl border bg-card p-3 sm:flex-row sm:items-center'>
        <div className='flex rounded-lg bg-muted p-1'>
          <button
            type='button'
            onClick={() => onSearchChange({ view: 'models' })}
            className={`flex items-center gap-2 rounded-md px-3 py-2 font-medium text-sm ${search.view === 'models' ? 'bg-background shadow-sm' : 'text-muted-foreground'}`}
          >
            <Boxes className='size-4' /> 全部模型
          </button>
          <button
            type='button'
            onClick={() => onSearchChange({ view: 'connections' })}
            className={`flex items-center gap-2 rounded-md px-3 py-2 font-medium text-sm ${search.view === 'connections' ? 'bg-background shadow-sm' : 'text-muted-foreground'}`}
          >
            <Unplug className='size-4' /> 连接管理
          </button>
        </div>
        <div className='relative min-w-0 flex-1 sm:max-w-sm'>
          <Search className='absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground' />
          <Input
            aria-label='搜索模型或连接'
            className='pl-9'
            value={search.q}
            placeholder={
              search.view === 'models'
                ? '搜索模型、上游名称或连接'
                : '搜索连接名称或供应商'
            }
            onChange={(event) => onSearchChange({ q: event.target.value })}
          />
        </div>
      </div>

      <div className='flex flex-wrap gap-3'>
        {search.view === 'models' ? (
          <>
            <FilterSelect
              label='模型类型'
              value={search.type}
              options={[
                ['all', '全部类型'],
                ['embedding', 'Embedding'],
                ['rerank', 'Rerank'],
              ]}
              onChange={(type) =>
                onSearchChange({ type: type as ModelServiceSearch['type'] })
              }
            />
            <FilterSelect
              label='状态'
              value={search.status}
              options={[
                ['all', '全部状态'],
                ['active', '可用'],
                ['disabled', '已停用'],
              ]}
              onChange={(status) =>
                onSearchChange({
                  status: status as ModelServiceSearch['status'],
                })
              }
            />
          </>
        ) : (
          <FilterSelect
            label='支持能力'
            value={search.capability}
            options={[
              ['all', '全部能力'],
              ['embedding', 'Embedding'],
              ['rerank', 'Rerank'],
              ['parser', '文档解析'],
            ]}
            onChange={(capability) =>
              onSearchChange({
                capability: capability as ModelServiceSearch['capability'],
              })
            }
          />
        )}
        {scope === 'workspace' && (
          <FilterSelect
            label='归属'
            value={search.scope}
            options={[
              ['all', '全部归属'],
              ['workspace', '当前工作区'],
              ['platform', '平台共享'],
            ]}
            onChange={(nextScope) =>
              onSearchChange({
                scope: nextScope as ModelServiceSearch['scope'],
              })
            }
          />
        )}
      </div>

      {search.view === 'models' ? (
        <ModelCatalog
          models={modelsQuery.data ?? []}
          routeScope={scope}
          workspaceSlug={workspaceSlug}
          isPending={modelsQuery.isPending}
        />
      ) : (
        <ProviderCatalog
          providers={providers}
          routeScope={scope}
          workspaceSlug={workspaceSlug}
          isPending={providersQuery.isPending}
        />
      )}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>配置新连接</DialogTitle>
            <DialogDescription>
              连接的能力由服务端决定；一条连接可承载多个模型。
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

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: [string, string][]
  onChange: (value: string) => void
}) {
  return (
    <label className='flex items-center gap-2 text-muted-foreground text-sm'>
      <span>{label}</span>
      <select
        className='h-9 rounded-md border bg-background px-3 text-foreground'
        value={value}
        onChange={(event) => onChange(event.target.value)}
      >
        {options.map(([option, text]) => (
          <option key={option} value={option}>
            {text}
          </option>
        ))}
      </select>
    </label>
  )
}
