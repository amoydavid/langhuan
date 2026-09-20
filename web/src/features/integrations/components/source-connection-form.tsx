import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { KeyRound, Loader2 } from 'lucide-react'
import type { Control } from 'react-hook-form'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { parseApiError } from '@/lib/api/error'
import {
  secretFieldLabelKey,
  secretFieldPlaceholderKey,
  testableProviders,
} from '../provider'
import { createSourceConnectionMutationOptions } from '../queries'
import {
  createSourceConnectionFormSchema,
  toCreateSourceConnectionInput,
} from '../schemas'
import type { SourceConnection, SourceConnectionFormValues } from '../types'

const providerFormLabelKeys = {
  feishu: 'integrations.form.providerFeishu',
  firecrawl: 'integrations.form.providerFirecrawl',
  jina: 'integrations.form.providerJina',
  browser: 'integrations.form.providerBrowser',
} as const

type SourceConnectionFormProps = {
  workspaceSlug: string
  // 编辑模式预留：首版仅实现 create。
  connection?: SourceConnection
}

export function SourceConnectionForm({
  workspaceSlug,
}: SourceConnectionFormProps) {
  // TODO: 编辑模式（connection 不为空时）暂未实现，首版仅支持 create。
  // 后续如需编辑可在此分支复用 zodResolver(updateSourceConnectionSchema)。
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { t } = useTranslation()

  const form = useForm<SourceConnectionFormValues>({
    resolver: zodResolver(createSourceConnectionFormSchema),
    defaultValues: {
      provider: 'feishu',
      name: '',
      app_id: '',
      app_secret: '',
      endpoint_url: '',
    },
  })
  const provider = form.watch('provider')

  const mutation = useMutation(
    createSourceConnectionMutationOptions(workspaceSlug)
  )

  async function onSubmit(values: SourceConnectionFormValues) {
    try {
      await mutation.mutateAsync(toCreateSourceConnectionInput(values))
      await queryClient.invalidateQueries({
        queryKey: ['source-connections', workspaceSlug],
      })
      // 连接测试需要已保存的连接 ID，新建时不存在——保存后引导去列表卡片测试。
      toast.success(t('integrations.form.createdToast'), {
        description: testableProviders.has(values.provider)
          ? t('integrations.form.createdToastTestHint')
          : undefined,
      })
      await navigate({
        to: `/workspaces/${encodeURIComponent(workspaceSlug)}/integrations`,
        replace: true,
      })
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  const control = form.control

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='grid gap-5'>
        <ProviderRadioField control={control} />
        <NameField control={control} />
        {provider !== 'browser' ? (
          <>
            {provider === 'feishu' && <AppIdField control={control} />}
            <SecretField control={control} provider={provider} />
            {provider === 'firecrawl' && (
              <EndpointField control={control} provider={provider} />
            )}
          </>
        ) : (
          <>
            <EndpointField control={control} provider={provider} />
            {/* browser 的凭证（Token）可选，置于 endpoint 之后展示。 */}
            <SecretField control={control} provider={provider} />
          </>
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
            <KeyRound />
          )}
          {t('integrations.form.submitButton')}
        </Button>
      </form>
    </Form>
  )
}

function ProviderRadioField({
  control,
}: {
  control: Control<SourceConnectionFormValues>
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={control}
      name='provider'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('integrations.form.providerLabel')}</FormLabel>
          <FormControl>
            <RadioGroup
              value={field.value}
              onValueChange={field.onChange}
              className='grid gap-2 sm:grid-cols-4'
            >
              {(
                Object.keys(
                  providerFormLabelKeys
                ) as (keyof typeof providerFormLabelKeys)[]
              ).map((value) => (
                // 原生 label htmlFor → item id：整卡可点（理由同 web 源导入模式）。
                <label
                  key={value}
                  htmlFor={`source-connection-provider-${value}`}
                  className='flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 font-normal has-[button[data-state=checked]]:border-primary'
                >
                  <RadioGroupItem
                    value={value}
                    id={`source-connection-provider-${value}`}
                  />
                  {t(providerFormLabelKeys[value])}
                </label>
              ))}
            </RadioGroup>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function NameField({
  control,
}: {
  control: Control<SourceConnectionFormValues>
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={control}
      name='name'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('integrations.form.nameLabel')}</FormLabel>
          <FormControl>
            <Input
              autoFocus
              placeholder={t('integrations.form.namePlaceholder')}
              {...field}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function AppIdField({
  control,
}: {
  control: Control<SourceConnectionFormValues>
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={control}
      name='app_id'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('integrations.form.appIdLabel')}</FormLabel>
          <FormControl>
            <Input
              placeholder={t('integrations.form.appIdPlaceholder')}
              {...field}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** 凭证字段：feishu=App Secret、firecrawl/jina=API Key（必填）、browser=Token（可选）。 */
function SecretField({
  control,
  provider,
}: {
  control: Control<SourceConnectionFormValues>
  provider: SourceConnectionFormValues['provider']
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={control}
      name='app_secret'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(secretFieldLabelKey(provider))}</FormLabel>
          <FormControl>
            <Input
              type='password'
              autoComplete='new-password'
              placeholder={t(secretFieldPlaceholderKey(provider))}
              {...field}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** endpoint 字段：firecrawl 可选 http/https、browser 必填 ws/wss。 */
function EndpointField({
  control,
  provider,
}: {
  control: Control<SourceConnectionFormValues>
  provider: SourceConnectionFormValues['provider']
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={control}
      name='endpoint_url'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('integrations.form.endpointLabel')}</FormLabel>
          <FormControl>
            <Input
              placeholder={t(
                provider === 'firecrawl'
                  ? 'integrations.form.endpointFirecrawlPlaceholder'
                  : 'integrations.form.endpointBrowserPlaceholder'
              )}
              {...field}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
