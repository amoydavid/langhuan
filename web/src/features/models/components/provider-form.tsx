import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Loader2, PlugZap, RotateCcw } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { parseApiError } from '@/lib/api/error'
import {
  createModelProvider,
  getModelProviderOptions,
  updateModelProvider,
} from '../api'
import { invalidateSelectableModels } from '../cache'
import { type ProviderFormValues, providerFormSchema } from '../schemas'
import type { ModelProvider, ModelScope, ProviderKey } from '../types'
import { Field, ProviderFields } from './provider-fields'
import {
  providerFormDefaults,
  providerLabels,
  toCreateProviderRequest,
  toUpdateProviderRequest,
} from './provider-form-data'

function clearCredentials(
  form: ReturnType<typeof useForm<ProviderFormValues>>,
  provider: ProviderKey
) {
  switch (provider) {
    case 'openai':
      form.setValue('custom_headers', '')
      form.setValue('api_key', '')
      break
    case 'dashscope':
      form.setValue('api_key', '')
      break
    case 'ark':
      form.setValue('api_key', '')
      form.setValue('access_key', '')
      form.setValue('secret_key', '')
      break
    case 'tencentcloud':
      form.setValue('secret_id', '')
      form.setValue('secret_key', '')
      break
    case 'ollama':
      break
    case 'rerank_compatible':
      form.setValue('api_key', '')
      form.setValue('custom_headers', '')
      break
    case 'siliconflow':
      form.setValue('api_key', '')
      break
    case 'mineru':
      form.setValue('token', '')
      break
  }
}

type ProviderFormProps = {
  scope: ModelScope
  workspaceSlug?: string
  provider?: ModelProvider
  onSaved?: (provider: ModelProvider) => void
}

export function ProviderForm({
  scope,
  workspaceSlug,
  provider,
  onSaved,
}: ProviderFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initialProvider = provider?.provider ?? 'openai'
  const form = useForm<ProviderFormValues>({
    // zodResolver 与 discriminated union 的类型推断在 TS 严格模式下不完美，
    // 这里用 as 绕过 Resolver 类型不兼容问题。
    resolver: zodResolver(providerFormSchema) as never,
    defaultValues: providerFormDefaults(scope, initialProvider, provider),
    shouldUnregister: false,
  })
  const selectedProvider = form.watch('provider') ?? initialProvider
  const replaceCredentials =
    form.watch('replace_credentials') ?? provider === undefined
  const authMode = form.watch('auth_mode') ?? 'api_key'
  const openAIMode = form.watch('mode') ?? 'standard'
  const mutation = useMutation({
    mutationFn: async () => {
      const values = form.getValues()
      try {
        return provider
          ? await updateModelProvider(
              scope,
              provider.id,
              toUpdateProviderRequest(values),
              workspaceSlug
            )
          : await createModelProvider(
              scope,
              toCreateProviderRequest(values),
              workspaceSlug
            )
      } finally {
        clearCredentials(form, values.provider)
      }
    },
    onSuccess: async (saved) => {
      await Promise.all([
        invalidateSelectableModels(queryClient, scope, workspaceSlug),
        queryClient.invalidateQueries({
          queryKey: ['model-providers', scope],
        }),
        ...(provider
          ? [
              queryClient.invalidateQueries({
                queryKey: [
                  'model-provider',
                  scope,
                  workspaceSlug ?? null,
                  provider.id,
                ],
              }),
            ]
          : []),
      ])
      toast.success(
        provider
          ? t('models.providerForm.updatedToast')
          : t('models.providerForm.createdToast')
      )
      if (!provider) {
        form.reset(providerFormDefaults(scope, saved.provider))
      }
      onSaved?.(saved)
    },
  })

  const labels = providerLabels(t)
  // 从后端获取当前可用的 provider 选项（含 capability；mineru 仅在 mineru.enabled=true 时返回），
  // 据此过滤下拉选项，避免展示后端拒绝创建的 Provider。
  const { data: providerOptionsData = [] } = useQuery({
    queryKey: ['model-provider-options', scope, workspaceSlug ?? null],
    queryFn: () => getModelProviderOptions(scope, workspaceSlug),
    enabled: !provider,
    staleTime: 60_000,
  })
  const availableKeys = new Set(providerOptionsData.map((option) => option.key))
  const providerOptions = (Object.keys(labels) as ProviderKey[]).filter(
    (key) =>
      (scope === 'platform' || key !== 'ollama') &&
      (provider !== undefined ||
        providerOptionsData.length === 0 ||
        availableKeys.has(key))
  )
  const selectedCapabilities = providerOptionsData.find(
    (option) => option.key === selectedProvider
  )?.capabilities

  return (
    <form
      className='grid gap-5'
      onSubmit={form.handleSubmit(() => mutation.mutate())}
    >
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field
          label={t('models.providerForm.nameLabel')}
          htmlFor='provider-name'
        >
          <Input
            id='provider-name'
            disabled={provider !== undefined}
            {...form.register('name')}
          />
        </Field>
        <Field
          label={t('models.providerForm.displayNameLabel')}
          htmlFor='provider-display-name'
        >
          <Input
            id='provider-display-name'
            {...form.register('display_name')}
          />
        </Field>
      </div>
      <Field label='Provider' htmlFor='provider-key'>
        <select
          id='provider-key'
          className='h-9 rounded-md border bg-background px-3 text-sm'
          disabled={provider !== undefined}
          value={selectedProvider}
          onChange={(event) => {
            const next = event.target.value as ProviderKey
            form.reset(providerFormDefaults(scope, next))
          }}
        >
          {providerOptions.map((key) => (
            <option key={key} value={key}>
              {labels[key]}
            </option>
          ))}
        </select>
      </Field>
      {selectedCapabilities && selectedCapabilities.length > 0 && (
        <fieldset className='flex flex-wrap items-center gap-2'>
          <legend className='sr-only'>支持能力</legend>
          <span aria-hidden='true' className='text-muted-foreground text-xs'>
            支持能力
          </span>
          {selectedCapabilities.map((capability) => (
            <Badge key={capability} variant='secondary'>
              {capability === 'embedding'
                ? 'Embedding'
                : capability === 'rerank'
                  ? 'Rerank'
                  : '文档解析'}
            </Badge>
          ))}
        </fieldset>
      )}
      <ProviderFields
        form={form}
        provider={selectedProvider}
        replaceCredentials={replaceCredentials}
        authMode={authMode}
        openAIMode={openAIMode}
      />
      <Field
        label={t('models.providerForm.descriptionLabel')}
        htmlFor='provider-description'
      >
        <Textarea
          id='provider-description'
          className='min-h-20'
          {...form.register('description')}
        />
      </Field>
      {provider && provider.provider !== 'ollama' && (
        <div className='rounded-lg border bg-muted/30 p-4'>
          {!replaceCredentials ? (
            <div className='flex items-center justify-between gap-4'>
              <div>
                <p className='flex items-center gap-2 font-medium text-sm'>
                  <KeyRound className='size-4 text-emerald-600' />
                  {t('models.providerForm.credentialsConfigured')}
                </p>
                <p className='mt-1 text-muted-foreground text-xs'>
                  {t('models.providerForm.credentialsNotLoaded')}
                </p>
              </div>
              <Button
                type='button'
                variant='outline'
                onClick={() => form.setValue('replace_credentials', true)}
              >
                <RotateCcw />
                {t('models.providerForm.replaceCredentialsButton')}
              </Button>
            </div>
          ) : null}
        </div>
      )}
      {mutation.isError && (
        <p className='text-destructive text-sm' role='alert'>
          {parseApiError(mutation.error).message}
        </p>
      )}
      <Button className='w-fit' disabled={mutation.isPending}>
        {mutation.isPending ? (
          <Loader2 className='animate-spin' />
        ) : (
          <PlugZap />
        )}
        {t('models.providerForm.submitButton')}
      </Button>
    </form>
  )
}
