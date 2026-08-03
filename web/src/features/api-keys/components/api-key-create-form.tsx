import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from '@tanstack/react-router'
import { Check, KeyRound, Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { parseApiError } from '@/lib/api/error'
import { apiKeyScopeLabel, apiKeyScopeOrder } from '../display'
import { useCreateAPIKey } from '../queries'
import { type APIKeyCreateFormValues, apiKeyCreateSchema } from '../schemas'
import type { APIKeyCreatedEnvelope } from '../types'
import { APIKeySecretPanel } from './api-key-secret-panel'

const DEFAULT_EXPIRATION_DAYS = 90

type APIKeyCreateFormProps = {
  workspaceSlug: string
  knowledgeBases: { id: string; name: string }[]
}

export function APIKeyCreateForm({
  workspaceSlug,
  knowledgeBases,
}: APIKeyCreateFormProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  // useCreateAPIKey 在成功后自动失效 api-keys 列表缓存。
  const createMutation = useCreateAPIKey(workspaceSlug)
  // 创建响应的一次性明文只存活于本组件本地 state，绝不进缓存。
  const [created, setCreated] = useState<APIKeyCreatedEnvelope>()

  const form = useForm<APIKeyCreateFormValues>({
    resolver: zodResolver(apiKeyCreateSchema),
    defaultValues: {
      name: '',
      knowledge_base_ids: [],
      scopes: [],
      expiration: { type: 'days', days: DEFAULT_EXPIRATION_DAYS },
    },
  })

  // 监听 scope 勾选值，便于控制 disabled 状态。
  const watchedScopes = form.watch('scopes')
  const selectedScopeSet = useMemo(
    () => new Set(watchedScopes),
    [watchedScopes]
  )

  function toggleArrayValue<T extends string>(values: T[], value: T): T[] {
    return values.includes(value)
      ? values.filter((v) => v !== value)
      : [...values, value]
  }

  async function onSubmit(values: APIKeyCreateFormValues) {
    try {
      const result = await createMutation.mutateAsync(values)
      setCreated(result)
      form.reset()
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('apiKeys.createForm.nameLabel')}</FormLabel>
                <FormControl>
                  <Input
                    autoFocus
                    placeholder={t('apiKeys.createForm.namePlaceholder')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('apiKeys.createForm.nameDescription')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='knowledge_base_ids'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('apiKeys.createForm.knowledgeBasesLabel')}
                </FormLabel>
                <FormDescription>
                  {t('apiKeys.createForm.knowledgeBasesDescription')}
                </FormDescription>
                <div className='grid gap-2'>
                  {knowledgeBases.length === 0 ? (
                    <p className='text-muted-foreground text-sm'>
                      {t('apiKeys.createForm.noKnowledgeBases')}
                    </p>
                  ) : (
                    knowledgeBases.map((kb) => {
                      const checked = field.value.includes(kb.id)
                      return (
                        <Label
                          key={kb.id}
                          className='flex cursor-pointer items-center gap-3 rounded-md border p-3 has-[[data-state=checked]]:border-primary'
                        >
                          <Checkbox
                            checked={checked}
                            onCheckedChange={() =>
                              field.onChange(
                                toggleArrayValue(field.value, kb.id)
                              )
                            }
                          />
                          <span className='text-sm'>{kb.name}</span>
                        </Label>
                      )
                    })
                  )}
                </div>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='scopes'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('apiKeys.createForm.scopesLabel')}</FormLabel>
                <FormDescription>
                  {t('apiKeys.createForm.scopesDescription')}
                </FormDescription>
                <div className='grid gap-2 sm:grid-cols-2'>
                  {apiKeyScopeOrder.map((scope) => {
                    const checked = selectedScopeSet.has(scope)
                    return (
                      <Label
                        key={scope}
                        className='flex cursor-pointer items-center gap-3 rounded-md border p-3 has-[[data-state=checked]]:border-primary'
                      >
                        <Checkbox
                          checked={checked}
                          onCheckedChange={() =>
                            field.onChange(toggleArrayValue(field.value, scope))
                          }
                        />
                        <div>
                          <div className='text-sm'>
                            {apiKeyScopeLabel(t)[scope]}
                          </div>
                          <div className='font-mono text-muted-foreground text-xs'>
                            {scope}
                          </div>
                        </div>
                      </Label>
                    )
                  })}
                </div>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='expiration'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('apiKeys.createForm.expirationLabel')}</FormLabel>
                <FormDescription>
                  {t('apiKeys.createForm.expirationDescription')}
                </FormDescription>
                <div className='flex flex-wrap items-center gap-4'>
                  <Label className='flex cursor-pointer items-center gap-2'>
                    <input
                      type='radio'
                      className='size-4'
                      checked={field.value.type === 'days'}
                      onChange={() =>
                        field.onChange({
                          type: 'days',
                          days: DEFAULT_EXPIRATION_DAYS,
                        })
                      }
                    />
                    <span className='text-sm'>
                      {t('apiKeys.createForm.fixedDays')}
                    </span>
                  </Label>
                  <Label className='flex cursor-pointer items-center gap-2'>
                    <input
                      type='radio'
                      className='size-4'
                      checked={field.value.type === 'never'}
                      onChange={() => field.onChange({ type: 'never' })}
                    />
                    <span className='text-sm'>
                      {t('apiKeys.createForm.never')}
                    </span>
                  </Label>
                  {field.value.type === 'days' && (
                    <Input
                      type='number'
                      min={1}
                      max={365}
                      step={1}
                      className='w-28'
                      aria-label={t('apiKeys.createForm.daysAriaLabel')}
                      value={field.value.days}
                      onChange={(event) =>
                        field.onChange({
                          type: 'days',
                          days: event.target.valueAsNumber,
                        })
                      }
                    />
                  )}
                </div>
                <FormMessage />
              </FormItem>
            )}
          />

          {createMutation.isError && (
            <p className='text-destructive text-sm' role='alert'>
              {parseApiError(createMutation.error).message}
            </p>
          )}
          <Button disabled={createMutation.isPending}>
            {createMutation.isPending ? (
              <Loader2 className='animate-spin' />
            ) : (
              <KeyRound />
            )}
            {t('apiKeys.createForm.submit')}
          </Button>
        </form>
      </Form>

      <Dialog
        open={created !== undefined}
        onOpenChange={(open) => {
          if (!open) setCreated(undefined)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('apiKeys.createForm.createdTitle')}</DialogTitle>
            <DialogDescription>
              {t('apiKeys.createForm.createdDescription')}
            </DialogDescription>
          </DialogHeader>
          {created && (
            <div className='space-y-3'>
              <APIKeySecretPanel
                key={created.item.id}
                workspaceSlug={workspaceSlug}
                apiKeyId={created.item.id}
                initialSecret={created.api_key}
              />
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setCreated(undefined)}>
              {t('apiKeys.createForm.close')}
            </Button>
            <Button
              onClick={() => {
                if (!created) return
                const id = created.item.id
                setCreated(undefined)
                void navigate({
                  to: '/workspaces/$workspaceSlug/api-keys/$apiKeyId',
                  params: { workspaceSlug, apiKeyId: id },
                })
              }}
            >
              <Check />
              {t('apiKeys.createForm.viewDetails')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
