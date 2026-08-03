import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { ArrowLeft, Ban, Pencil } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'
import { parseApiError } from '@/lib/api/error'
import { APIKeyEditForm } from './components/api-key-edit-form'
import { APIKeyRevokeDialog } from './components/api-key-revoke-dialog'
import { APIKeySecretPanel } from './components/api-key-secret-panel'
import { APIKeyStatusBadge } from './components/api-key-status-badge'
import { apiKeyScopeLabel, apiKeyScopeOrder } from './display'
import { formatDateTime, formatExpiry } from './format'
import { useRevokeAPIKey } from './queries'
import type { APIKeyDetailEnvelope, APIKeyScope } from './types'

type APIKeyDetailPageProps = {
  data: APIKeyDetailEnvelope
}

function sortedScopes(scopes: APIKeyScope[]) {
  return [...scopes].sort(
    (a, b) => apiKeyScopeOrder.indexOf(a) - apiKeyScopeOrder.indexOf(b)
  )
}

function Field({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className='grid grid-cols-3 gap-2 py-2'>
      <dt className='text-muted-foreground text-sm'>{label}</dt>
      <dd className='col-span-2 text-sm'>{children}</dd>
    </div>
  )
}

export function APIKeyDetailPage({ data }: APIKeyDetailPageProps) {
  const { t } = useTranslation()
  const { workspaceSlug } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/api-keys/$apiKeyId',
  })
  const navigate = useNavigate()
  const { item, base_url, rest_base_url, mcp_url } = data
  const [revokeOpen, setRevokeOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const revokeMutation = useRevokeAPIKey(workspaceSlug, item.id)
  // 编辑表单的 KB 选项需要当前 workspace 全部 KB 列表。
  const { data: knowledgeBasesData } = useQuery(
    knowledgeBasesQueryOptions(workspaceSlug)
  )
  const knowledgeBases = (knowledgeBasesData ?? []).map((kb) => ({
    id: kb.id,
    name: kb.name,
  }))

  // 失效状态（已吊销/已过期）仍允许 owner/admin reveal 以复制历史 key，
  // 但复制不会恢复授权。active/expiring/expired/revoked 均可 reveal。

  return (
    <div className='space-y-6'>
      <div>
        <Button variant='ghost' size='sm' asChild className='mb-2'>
          <Link
            to='/workspaces/$workspaceSlug/api-keys'
            params={{ workspaceSlug }}
          >
            <ArrowLeft />
            {t('apiKeys.detailPage.backToList')}
          </Link>
        </Button>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div>
            <p className='page-eyebrow'>{t('apiKeys.detailPage.eyebrow')}</p>
            <h1 className='font-semibold text-2xl tracking-tight'>
              {item.name}
            </h1>
            <p className='mt-1 font-mono text-muted-foreground text-xs'>
              {item.token_prefix}…
            </p>
          </div>
          <div className='flex items-center gap-2'>
            <APIKeyStatusBadge status={item.status} />
            <Button
              variant='outline'
              size='sm'
              onClick={() => setEditOpen(true)}
              disabled={item.status === 'revoked'}
            >
              <Pencil />
              {t('apiKeys.detailPage.editButton')}
            </Button>
          </div>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('apiKeys.detailPage.secretTitle')}</CardTitle>
          <CardDescription>
            {t('apiKeys.detailPage.secretDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-3'>
          <APIKeySecretPanel workspaceSlug={workspaceSlug} apiKeyId={item.id} />
          {(item.status === 'revoked' || item.status === 'expired') && (
            <p className='font-medium text-amber-700 text-sm dark:text-amber-400'>
              {t('apiKeys.detailPage.revokedNotice')}
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('apiKeys.detailPage.detailsTitle')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className='divide-y'>
            <Field label={t('apiKeys.detailPage.fieldName')}>{item.name}</Field>
            <Field label={t('apiKeys.detailPage.fieldPrefix')}>
              <span className='font-mono'>{item.token_prefix}…</span>
            </Field>
            <Field label={t('apiKeys.detailPage.fieldKnowledgeBases')}>
              {item.knowledge_bases.length === 0 ? (
                <span className='text-muted-foreground'>
                  {t('apiKeys.detailPage.noKnowledgeBases')}
                </span>
              ) : (
                <div className='flex flex-wrap gap-1'>
                  {item.knowledge_bases.map((kb) => (
                    <Badge key={kb.id} variant='outline'>
                      {kb.name}
                    </Badge>
                  ))}
                </div>
              )}
            </Field>
            <Field label={t('apiKeys.detailPage.fieldScopes')}>
              <div className='flex flex-wrap gap-1'>
                {sortedScopes(item.scopes).map((scope) => (
                  <Badge key={scope} variant='secondary'>
                    {apiKeyScopeLabel(t)[scope]}
                  </Badge>
                ))}
              </div>
            </Field>
            <Field label={t('apiKeys.detailPage.fieldExpiry')}>
              {formatExpiry(item.expires_at)}
            </Field>
            <Field label={t('apiKeys.detailPage.fieldLastUsed')}>
              {item.last_used_at
                ? formatDateTime(item.last_used_at)
                : t('apiKeys.detailPage.neverUsed')}
            </Field>
            <Field label={t('apiKeys.detailPage.fieldCreatedBy')}>
              {item.created_by?.nickname ??
                t('apiKeys.detailPage.unknownCreator')}
            </Field>
            <Field label={t('apiKeys.detailPage.fieldCreatedAt')}>
              {formatDateTime(item.created_at)}
            </Field>
            {item.revoked_at && (
              <Field label={t('apiKeys.detailPage.fieldRevokedAt')}>
                {formatDateTime(item.revoked_at)}
              </Field>
            )}
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('apiKeys.detailPage.endpointsTitle')}</CardTitle>
          <CardDescription>
            {t('apiKeys.detailPage.endpointsDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <dl className='divide-y'>
            <Field label='Base URL'>
              <code className='break-all text-xs'>{base_url}</code>
            </Field>
            <Field label='REST'>
              <code className='break-all text-xs'>{rest_base_url}</code>
            </Field>
            <Field label='MCP'>
              <code className='break-all text-xs'>{mcp_url}</code>
            </Field>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('apiKeys.detailPage.dangerTitle')}</CardTitle>
          <CardDescription>
            {t('apiKeys.detailPage.dangerDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            variant='destructive'
            onClick={() => setRevokeOpen(true)}
            disabled={item.status === 'revoked'}
          >
            <Ban />
            {t('apiKeys.detailPage.revokeButton')}
          </Button>
        </CardContent>
      </Card>

      <APIKeyRevokeDialog
        open={revokeOpen}
        onOpenChange={setRevokeOpen}
        apiKeyName={item.name}
        isLoading={revokeMutation.isPending}
        error={
          revokeMutation.isError
            ? parseApiError(revokeMutation.error).message
            : undefined
        }
        onConfirm={() => {
          revokeMutation.mutate(undefined, {
            onSuccess: () => {
              setRevokeOpen(false)
              void navigate({
                to: '/workspaces/$workspaceSlug/api-keys',
                params: { workspaceSlug },
              })
            },
          })
        }}
      />

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('apiKeys.editDialog.title')}</DialogTitle>
            <DialogDescription>
              {t('apiKeys.editDialog.description')}
            </DialogDescription>
          </DialogHeader>
          {/* editOpen 控制挂载，保证每次打开都从最新 item 重新推导表单默认值 */}
          {editOpen && (
            <APIKeyEditForm
              key={item.id}
              workspaceSlug={workspaceSlug}
              apiKeyId={item.id}
              apiKey={item}
              knowledgeBases={knowledgeBases}
              onUpdated={() => setEditOpen(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
