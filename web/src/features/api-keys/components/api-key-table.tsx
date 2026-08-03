import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { apiKeyScopeLabel, apiKeyScopeOrder } from '../display'
import { formatDateTime, formatExpiry } from '../format'
import type { APIKey } from '../types'
import { APIKeyStatusBadge } from './api-key-status-badge'

type APIKeyTableProps = {
  workspaceSlug: string
  items: APIKey[]
}

function sortedScopes(scopes: APIKey['scopes']) {
  return [...scopes].sort(
    (a, b) => apiKeyScopeOrder.indexOf(a) - apiKeyScopeOrder.indexOf(b)
  )
}

export function APIKeyTable({ workspaceSlug, items }: APIKeyTableProps) {
  const { t } = useTranslation()
  if (items.length === 0) {
    return (
      <div className='rounded-lg border border-dashed p-10 text-center text-muted-foreground text-sm'>
        {t('apiKeys.table.empty')}
      </div>
    )
  }

  return (
    <>
      <div className='hidden overflow-hidden rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('apiKeys.table.headNamePrefix')}</TableHead>
              <TableHead>{t('apiKeys.table.headKnowledgeBases')}</TableHead>
              <TableHead>{t('apiKeys.table.headScopes')}</TableHead>
              <TableHead>{t('apiKeys.table.headExpiry')}</TableHead>
              <TableHead>{t('apiKeys.table.headLastUsed')}</TableHead>
              <TableHead>{t('apiKeys.table.headStatus')}</TableHead>
              <TableHead className='text-right'>
                {t('apiKeys.table.headView')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((apiKey) => (
              <TableRow key={apiKey.id}>
                <TableCell>
                  <div className='font-medium'>{apiKey.name}</div>
                  <div className='mt-1 font-mono text-muted-foreground text-xs'>
                    {apiKey.token_prefix}…
                  </div>
                </TableCell>
                <TableCell>
                  {apiKey.knowledge_bases.length === 0 ? (
                    <span className='text-muted-foreground text-sm'>
                      {t('apiKeys.table.noKnowledgeBases')}
                    </span>
                  ) : (
                    <div className='flex max-w-xs flex-wrap gap-1'>
                      {apiKey.knowledge_bases.map((kb) => (
                        <Badge key={kb.id} variant='outline'>
                          {kb.name}
                        </Badge>
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  <div className='flex max-w-xs flex-wrap gap-1'>
                    {sortedScopes(apiKey.scopes).map((scope) => (
                      <Badge key={scope} variant='secondary'>
                        {apiKeyScopeLabel(t)[scope]}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell className='whitespace-nowrap text-sm'>
                  {formatExpiry(apiKey.expires_at)}
                </TableCell>
                <TableCell className='text-sm'>
                  {apiKey.last_used_at
                    ? formatDateTime(apiKey.last_used_at)
                    : t('apiKeys.table.neverUsed')}
                </TableCell>
                <TableCell>
                  <APIKeyStatusBadge status={apiKey.status} />
                </TableCell>
                <TableCell className='text-right'>
                  <Button variant='ghost' size='icon' asChild>
                    <Link
                      to='/workspaces/$workspaceSlug/api-keys/$apiKeyId'
                      params={{
                        workspaceSlug,
                        apiKeyId: apiKey.id,
                      }}
                      aria-label={t('apiKeys.table.viewAriaLabel', {
                        name: apiKey.name,
                      })}
                    >
                      <ArrowRight />
                    </Link>
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className='grid gap-3 md:hidden'>
        {items.map((apiKey) => (
          <div
            key={apiKey.id}
            className='space-y-3 rounded-lg border bg-card p-4'
          >
            <div className='flex items-start justify-between gap-2'>
              <div className='min-w-0'>
                <div className='truncate font-medium'>{apiKey.name}</div>
                <div className='mt-1 font-mono text-muted-foreground text-xs'>
                  {apiKey.token_prefix}…
                </div>
              </div>
              <APIKeyStatusBadge status={apiKey.status} />
            </div>
            <div className='flex flex-wrap gap-1'>
              {sortedScopes(apiKey.scopes).map((scope) => (
                <Badge key={scope} variant='secondary'>
                  {apiKeyScopeLabel(t)[scope]}
                </Badge>
              ))}
            </div>
            <div className='flex items-center justify-between text-sm'>
              <span className='text-muted-foreground'>
                {t('apiKeys.table.mobileExpiry', {
                  expiry: formatExpiry(apiKey.expires_at),
                })}
              </span>
              <Button variant='outline' size='sm' asChild>
                <Link
                  to='/workspaces/$workspaceSlug/api-keys/$apiKeyId'
                  params={{ workspaceSlug, apiKeyId: apiKey.id }}
                >
                  {t('apiKeys.table.view')}
                </Link>
              </Button>
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
