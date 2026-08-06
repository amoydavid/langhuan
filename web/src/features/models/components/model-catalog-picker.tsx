import { useQuery } from '@tanstack/react-query'
import { Check, ChevronDown, Loader2, Search } from 'lucide-react'
import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { parseApiError } from '@/lib/api/error'
import { listProviderModelCatalog } from '../api'
import type {
  ModelProvider,
  ModelScope,
  ModelType,
  ProviderModelCatalogItem,
} from '../types'

type ModelCatalogPickerProps = {
  provider: ModelProvider
  scope: ModelScope
  workspaceSlug?: string
  type: ModelType
  onSelect: (item: ProviderModelCatalogItem) => void
}

/** On-demand Provider directory picker; selection only updates the parent form. */
export function ModelCatalogPicker({
  provider,
  scope,
  workspaceSlug,
  type,
  onSelect,
}: ModelCatalogPickerProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const catalogEnabled = provider.model_catalog === true
  const catalogQuery = useQuery({
    queryKey: [
      'provider-model-catalog',
      scope,
      workspaceSlug ?? null,
      provider.id,
      type,
      query,
    ],
    queryFn: () =>
      listProviderModelCatalog(scope, provider.id, type, workspaceSlug, query),
    enabled: open && catalogEnabled && provider.status === 'active',
    staleTime: 60_000,
  })

  if (!catalogEnabled) return null

  return (
    <div className='sm:col-span-2'>
      <Button
        type='button'
        variant='outline'
        className='min-h-11 w-full justify-between sm:w-auto'
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <span className='inline-flex items-center gap-2'>
          <ChevronDown className='size-4' />从 Provider 模型目录快速填充
        </span>
        {open && catalogQuery.isFetching ? (
          <Loader2 className='size-4 animate-spin' />
        ) : null}
      </Button>
      {open && (
        <div className='mt-2 rounded-lg border bg-background p-3 shadow-sm'>
          <div className='relative'>
            <Search className='pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground' />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder='搜索模型名称'
              aria-label='搜索模型目录'
              className='pl-9'
            />
          </div>
          {catalogQuery.isError && (
            <p className='mt-3 text-destructive text-sm' role='alert'>
              {parseApiError(catalogQuery.error).message}
            </p>
          )}
          {!catalogQuery.isPending &&
            !catalogQuery.isError &&
            catalogQuery.data?.items.length === 0 && (
              <p className='py-5 text-center text-muted-foreground text-sm'>
                没有匹配的模型，可继续手动填写。
              </p>
            )}
          <div className='mt-2 grid max-h-72 gap-1 overflow-y-auto'>
            {catalogQuery.data?.items.map((item) => (
              <button
                type='button'
                key={item.id}
                disabled={!item.available}
                className='flex min-h-11 items-start justify-between gap-3 rounded-md px-3 py-2 text-left hover:bg-muted/60 disabled:cursor-not-allowed disabled:opacity-50'
                onClick={() => {
                  onSelect(item)
                  setOpen(false)
                }}
              >
                <span className='min-w-0'>
                  <span className='block truncate font-medium text-sm'>
                    {item.display_name || item.id}
                  </span>
                  <span className='block truncate font-mono text-muted-foreground text-xs'>
                    {item.id}
                  </span>
                  {item.description && (
                    <span className='mt-1 block truncate text-muted-foreground text-xs'>
                      {item.description}
                    </span>
                  )}
                </span>
                <span className='flex shrink-0 items-center gap-2'>
                  {item.dimensions ? (
                    <Badge variant='outline'>{item.dimensions}d</Badge>
                  ) : null}
                  <Check className='size-4 text-primary' />
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
