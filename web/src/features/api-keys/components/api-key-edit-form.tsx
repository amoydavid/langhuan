import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, Save } from 'lucide-react'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { useUpdateAPIKey } from '../queries'
import { type APIKeyUpdateFormValues, apiKeyUpdateSchema } from '../schemas'
import type { APIKey } from '../types'

const DEFAULT_EXPIRATION_DAYS = 90

type APIKeyEditFormProps = {
  workspaceSlug: string
  apiKeyId: string
  apiKey: APIKey
  knowledgeBases: { id: string; name: string }[]
  onUpdated: () => void
}

// deriveUpdateDefaultValues 从当前 API Key 推导编辑表单初始值。
// expires_at 为 null -> never；否则按剩余天数向上取整（最少 1 天）。
function deriveUpdateDefaultValues(apiKey: APIKey): APIKeyUpdateFormValues {
  const name = apiKey.name
  const knowledge_base_ids = apiKey.knowledge_bases.map((kb) => kb.id)
  const scopes = [...apiKey.scopes]
  if (apiKey.expires_at == null) {
    return {
      name,
      knowledge_base_ids,
      scopes,
      expiration: { type: 'never' },
    }
  }
  const remainingMs = new Date(apiKey.expires_at).getTime() - Date.now()
  const remainingDays = Math.max(
    1,
    Math.ceil(remainingMs / (24 * 60 * 60 * 1000))
  )
  return {
    name,
    knowledge_base_ids,
    scopes,
    expiration: { type: 'days', days: remainingDays },
  }
}

export function APIKeyEditForm({
  workspaceSlug,
  apiKeyId,
  apiKey,
  knowledgeBases,
  onUpdated,
}: APIKeyEditFormProps) {
  const { t } = useTranslation()
  const updateMutation = useUpdateAPIKey(workspaceSlug, apiKeyId)

  const form = useForm<APIKeyUpdateFormValues>({
    resolver: zodResolver(apiKeyUpdateSchema),
    defaultValues: deriveUpdateDefaultValues(apiKey),
  })

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

  async function onSubmit(values: APIKeyUpdateFormValues) {
    try {
      await updateMutation.mutateAsync(values)
      onUpdated()
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('apiKeys.editForm.nameLabel')}</FormLabel>
              <FormControl>
                <Input autoFocus {...field} />
              </FormControl>
              <FormDescription>
                {t('apiKeys.editForm.nameDescription')}
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
              <FormLabel>{t('apiKeys.editForm.knowledgeBasesLabel')}</FormLabel>
              <FormDescription>
                {t('apiKeys.editForm.knowledgeBasesDescription')}
              </FormDescription>
              <div className='grid gap-2'>
                {knowledgeBases.length === 0 ? (
                  <p className='text-muted-foreground text-sm'>
                    {t('apiKeys.editForm.noKnowledgeBases')}
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
                            field.onChange(toggleArrayValue(field.value, kb.id))
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
              <FormLabel>{t('apiKeys.editForm.scopesLabel')}</FormLabel>
              <FormDescription>
                {t('apiKeys.editForm.scopesDescription')}
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
              <FormLabel>{t('apiKeys.editForm.expirationLabel')}</FormLabel>
              <FormDescription>
                {t('apiKeys.editForm.expirationDescription')}
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
                    {t('apiKeys.editForm.fixedDays')}
                  </span>
                </Label>
                <Label className='flex cursor-pointer items-center gap-2'>
                  <input
                    type='radio'
                    className='size-4'
                    checked={field.value.type === 'never'}
                    onChange={() => field.onChange({ type: 'never' })}
                  />
                  <span className='text-sm'>{t('apiKeys.editForm.never')}</span>
                </Label>
                {field.value.type === 'days' && (
                  <Input
                    type='number'
                    min={1}
                    max={365}
                    step={1}
                    className='w-28'
                    aria-label={t('apiKeys.editForm.daysAriaLabel')}
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

        {updateMutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(updateMutation.error).message}
          </p>
        )}
        <Button disabled={updateMutation.isPending}>
          {updateMutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <Save />
          )}
          {t('apiKeys.editForm.submit')}
        </Button>
      </form>
    </Form>
  )
}
