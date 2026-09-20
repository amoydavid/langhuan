import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Loader2, Pencil, Plug, PlugZap, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import type { Role } from '@/features/auth/types'
import { parseApiError } from '@/lib/api/error'
import { cn } from '@/lib/utils'
import {
  secretFieldLabelKey,
  secretFieldPlaceholderKey,
  sourceProviderBadgeKey,
  testableProviders,
} from '../provider'
import {
  deleteSourceConnectionMutationOptions,
  sourceConnectionsQueryOptions,
  testSourceConnectionMutationOptions,
  updateSourceConnectionMutationOptions,
} from '../queries'
import type { SourceConnection, SourceProvider } from '../types'

type SourceConnectionListProps = {
  workspaceSlug: string
  workspaceRole: Role
}

export function SourceConnectionList({
  workspaceSlug,
  workspaceRole,
}: SourceConnectionListProps) {
  const { t } = useTranslation()
  const { data: connections = [], isPending } = useQuery(
    sourceConnectionsQueryOptions(workspaceSlug)
  )

  const canManage = workspaceRole === 'owner' || workspaceRole === 'admin'

  if (isPending) {
    return <SourceConnectionListSkeleton />
  }

  if (connections.length === 0) {
    return (
      <Card className='border-dashed'>
        <CardContent className='flex min-h-48 flex-col items-center justify-center text-center'>
          <div className='mb-4 flex size-11 items-center justify-center rounded-lg bg-muted'>
            <Plug className='size-5 text-muted-foreground' />
          </div>
          <h2 className='font-medium'>{t('integrations.list.emptyTitle')}</h2>
          <p className='mt-2 max-w-md text-muted-foreground text-sm'>
            {t('integrations.list.emptyDescription')}
          </p>
          <Button asChild className='mt-5'>
            <Link
              to='/workspaces/$workspaceSlug/integrations/new'
              params={{ workspaceSlug }}
            >
              <Plus />
              {t('integrations.list.createFirstButton')}
            </Link>
          </Button>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
      {connections.map((connection) => (
        <SourceConnectionCard
          key={connection.id}
          connection={connection}
          workspaceSlug={workspaceSlug}
          canManage={canManage}
        />
      ))}
    </div>
  )
}

function SourceConnectionListSkeleton() {
  return (
    <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
      {Array.from({ length: 3 }).map((_, index) => (
        <Card key={`skeleton-${index}`}>
          <CardHeader>
            <Skeleton className='size-11 rounded-lg' />
            <Skeleton className='h-5 w-2/3' />
            <Skeleton className='h-4 w-1/3' />
          </CardHeader>
          <CardContent className='space-y-2'>
            <Skeleton className='h-4 w-full' />
            <Skeleton className='h-4 w-1/2' />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

type SourceConnectionCardProps = {
  connection: SourceConnection
  workspaceSlug: string
  canManage: boolean
}

function SourceConnectionCard({
  connection,
  workspaceSlug,
  canManage,
}: SourceConnectionCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isActive = connection.status === 'active'
  // 后端契约：探测成功与失败都 HTTP 200，ok=false 时 message 用户可读；
  // 「无法测试」（停用/feishu）走 error。结果非持久化，仅卡片临时展示。
  const [testResult, setTestResult] = useState<{
    ok: boolean
    message: string
  } | null>(null)
  const [testError, setTestError] = useState<string | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const toggleMutation = useMutation(
    updateSourceConnectionMutationOptions(workspaceSlug, connection.id)
  )
  const deleteMutation = useMutation(
    deleteSourceConnectionMutationOptions(workspaceSlug)
  )
  const testMutation = useMutation(
    testSourceConnectionMutationOptions(workspaceSlug, connection.id)
  )
  const canTest = canManage && testableProviders.has(connection.provider)

  function invalidateConnections() {
    void queryClient.invalidateQueries({
      queryKey: ['source-connections', workspaceSlug],
    })
  }

  async function handleToggle() {
    try {
      await toggleMutation.mutateAsync({
        status: isActive ? 'disabled' : 'active',
      })
      invalidateConnections()
      toast.success(
        t(
          isActive
            ? 'integrations.toggle.disabledToast'
            : 'integrations.toggle.enabledToast'
        )
      )
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  async function handleTest() {
    setTestResult(null)
    setTestError(null)
    try {
      const result = await testMutation.mutateAsync()
      setTestResult({ ok: result.ok, message: result.message })
    } catch (error) {
      setTestError(parseApiError(error).message)
    }
  }

  return (
    <>
      <Card className='group resource-card'>
        <CardHeader>
          <div className='icon-tile mb-3'>
            <Plug className='size-5' />
          </div>
          <CardTitle className='truncate'>{connection.name}</CardTitle>
          <CardAction className='flex items-center gap-2'>
            {connection.provider !== 'feishu' && (
              <Badge variant='outline'>
                {t(sourceProviderBadgeKey(connection.provider))}
              </Badge>
            )}
            <Badge variant={isActive ? 'default' : 'secondary'}>
              {t(
                isActive
                  ? 'integrations.card.statusActive'
                  : 'integrations.card.statusDisabled'
              )}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className='space-y-2 text-sm'>
          <SourceConnectionDetailRows connection={connection} />
          <div className='flex items-center justify-between gap-2'>
            <span className='text-muted-foreground'>
              {t('integrations.card.boundKbLabel')}
            </span>
            <span className='font-medium'>
              {t('integrations.card.boundKbPlaceholder')}
            </span>
          </div>
          {canTest && (
            <div className='flex flex-wrap items-center gap-2 pt-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={handleTest}
                disabled={testMutation.isPending}
              >
                {testMutation.isPending ? (
                  <Loader2 className='animate-spin' />
                ) : (
                  <PlugZap />
                )}
                {t('integrations.card.testButton')}
              </Button>
            </div>
          )}
          {(testResult || testError) && (
            <p
              role='status'
              className={cn(
                'text-xs',
                testResult?.ok
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-destructive'
              )}
            >
              {testResult ? testResult.message : testError}
            </p>
          )}
          {canManage && (
            <div className='flex flex-wrap items-center gap-2 pt-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setEditOpen(true)}
              >
                <Pencil />
                {t('integrations.card.editButton')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={handleToggle}
                disabled={toggleMutation.isPending}
              >
                {toggleMutation.isPending ? (
                  <Loader2 className='animate-spin' />
                ) : null}
                {isActive
                  ? t('integrations.card.disableButton')
                  : t('integrations.card.enableButton')}
              </Button>
              <Button
                variant='ghost'
                size='sm'
                className='text-destructive hover:text-destructive'
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 />
                {t('integrations.card.deleteButton')}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <SourceConnectionEditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        connection={connection}
        workspaceSlug={workspaceSlug}
        onUpdated={() => {
          invalidateConnections()
          setEditOpen(false)
          toast.success(t('integrations.editDialog.savedToast'))
        }}
      />

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('integrations.deleteDialog.title')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('integrations.deleteDialog.description')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteMutation.isError && (
            <p className='text-destructive text-sm' role='alert'>
              {parseApiError(deleteMutation.error).message}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('integrations.deleteDialog.cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              className={cn(buttonVariants({ variant: 'destructive' }))}
              disabled={deleteMutation.isPending}
              onClick={async (event) => {
                // 阻止 AlertDialog 默认关闭，交由 mutation 成功后控制。
                event.preventDefault()
                try {
                  await deleteMutation.mutateAsync(connection.id)
                  invalidateConnections()
                  setDeleteOpen(false)
                  toast.success(t('integrations.deleteDialog.deletedToast'))
                } catch (error) {
                  toast.error(parseApiError(error).message)
                }
              }}
            >
              {deleteMutation.isPending && <Loader2 className='animate-spin' />}
              {t('integrations.deleteDialog.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

/** 按 provider 展示非敏感明细：feishu=App ID，firecrawl/jina=掩码 key（+/endpoint），browser=endpoint。 */
function SourceConnectionDetailRows({
  connection,
}: {
  connection: SourceConnection
}) {
  const { t } = useTranslation()
  const provider = connection.provider

  const endpointRow =
    provider === 'firecrawl' || provider === 'browser' ? (
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground'>
          {t('integrations.card.endpointLabel')}
        </span>
        <span className='truncate font-mono text-xs'>
          {connection.endpoint_url === ''
            ? t('integrations.card.endpointDefault')
            : connection.endpoint_url}
        </span>
      </div>
    ) : null

  if (provider === 'feishu') {
    return (
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground'>
          {t('integrations.card.appIdLabel')}
        </span>
        <span className='truncate font-mono text-xs'>{connection.app_id}</span>
      </div>
    )
  }

  return (
    <>
      {(provider === 'firecrawl' || provider === 'jina') && (
        <div className='flex items-center justify-between gap-2'>
          <span className='text-muted-foreground'>
            {t('integrations.card.apiKeyLabel')}
          </span>
          <span className='font-mono text-xs'>
            {t('integrations.card.keyMasked')}
          </span>
        </div>
      )}
      {endpointRow}
    </>
  )
}

type SourceConnectionEditDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  connection: SourceConnection
  workspaceSlug: string
  onUpdated: () => void
}

function SourceConnectionEditDialog({
  open,
  onOpenChange,
  connection,
  workspaceSlug,
  onUpdated,
}: SourceConnectionEditDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState(connection.name)
  const [appSecret, setAppSecret] = useState('')
  const mutation = useMutation(
    updateSourceConnectionMutationOptions(workspaceSlug, connection.id)
  )
  const provider: SourceProvider = connection.provider

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    try {
      const payload: { name: string; app_secret?: string } = { name }
      if (appSecret.trim() !== '') {
        payload.app_secret = appSecret
      }
      await mutation.mutateAsync(payload)
      onUpdated()
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('integrations.editDialog.title')}</DialogTitle>
          <DialogDescription>
            {t('integrations.editDialog.description')}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className='space-y-4'>
          <div className='space-y-2'>
            <Label htmlFor='source-connection-edit-name'>
              {t('integrations.form.nameLabel')}
            </Label>
            <Input
              id='source-connection-edit-name'
              value={name}
              onChange={(event) => setName(event.target.value)}
              autoFocus
            />
          </div>
          {provider === 'feishu' && (
            <div className='space-y-2'>
              <Label htmlFor='source-connection-edit-app-id'>
                {t('integrations.form.appIdLabel')}
              </Label>
              <Input
                id='source-connection-edit-app-id'
                value={connection.app_id}
                readOnly
              />
            </div>
          )}
          <div className='space-y-2'>
            <Label htmlFor='source-connection-edit-app-secret'>
              {t(secretFieldLabelKey(provider))}
            </Label>
            <Input
              id='source-connection-edit-app-secret'
              type='password'
              autoComplete='new-password'
              placeholder={t(secretFieldPlaceholderKey(provider))}
              value={appSecret}
              onChange={(event) => setAppSecret(event.target.value)}
            />
            <p className='text-muted-foreground text-xs'>
              {t('integrations.form.appSecretEditHint')}
            </p>
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button
                type='button'
                variant='outline'
                disabled={mutation.isPending}
              >
                {t('integrations.deleteDialog.cancel')}
              </Button>
            </DialogClose>
            <Button type='submit' disabled={mutation.isPending}>
              {mutation.isPending ? (
                <Loader2 className='animate-spin' />
              ) : (
                <Pencil />
              )}
              {t('integrations.editDialog.saveButton')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
