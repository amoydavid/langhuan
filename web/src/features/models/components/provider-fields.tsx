import type { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import type { ProviderFormValues } from '../schemas'
import type { ProviderKey } from '../types'

type ProviderFieldsProps = {
  form: ReturnType<typeof useForm<ProviderFormValues>>
  provider: ProviderKey
  replaceCredentials: boolean
  authMode: string | undefined
  openAIMode: string | undefined
}

export function ProviderFields({
  form,
  provider,
  replaceCredentials,
  authMode,
  openAIMode,
}: ProviderFieldsProps) {
  const { t } = useTranslation()
  if (provider === 'openai') {
    return (
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field
          label={t('models.providerFields.modeLabel')}
          htmlFor='openai-mode'
        >
          <select
            id='openai-mode'
            className='h-9 rounded-md border bg-background px-3 text-sm'
            {...form.register('mode')}
          >
            <option value='standard'>Standard</option>
            <option value='azure'>Azure OpenAI</option>
          </select>
        </Field>
        <NumberField
          form={form}
          name='timeout_seconds'
          label={t('models.providerFields.timeoutLabel')}
        />
        <Field label='Base URL' htmlFor='openai-base-url'>
          <Input id='openai-base-url' {...form.register('base_url')} />
        </Field>
        {openAIMode === 'azure' && (
          <Field label='API Version' htmlFor='openai-api-version'>
            <Input id='openai-api-version' {...form.register('api_version')} />
          </Field>
        )}
        {replaceCredentials && (
          <>
            <Field label='API Key' htmlFor='openai-api-key'>
              <Input
                id='openai-api-key'
                type='password'
                autoComplete='new-password'
                {...form.register('api_key')}
              />
            </Field>
            <Field
              label={t('models.providerFields.customHeadersLabel')}
              htmlFor='openai-custom-headers'
            >
              <Textarea
                id='openai-custom-headers'
                aria-describedby='openai-custom-headers-help'
                className='min-h-20 font-mono'
                placeholder={'X-Tenant: tenant-id\nX-Gateway-Key: secret'}
                autoComplete='off'
                {...form.register('custom_headers')}
              />
              <p
                id='openai-custom-headers-help'
                className='text-muted-foreground text-xs'
              >
                {t('models.providerFields.customHeadersHelp')}
              </p>
            </Field>
          </>
        )}
      </div>
    )
  }
  if (provider === 'ark') {
    return (
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field
          label={t('models.providerFields.authModeLabel')}
          htmlFor='ark-auth-mode'
        >
          <select
            id='ark-auth-mode'
            className='h-9 rounded-md border bg-background px-3 text-sm'
            {...form.register('auth_mode')}
          >
            <option value='api_key'>API Key</option>
            <option value='ak_sk'>Access Key / Secret Key</option>
          </select>
        </Field>
        <Field label='Region' htmlFor='ark-region'>
          <Input id='ark-region' {...form.register('region')} />
        </Field>
        <Field label='Base URL' htmlFor='ark-base-url'>
          <Input id='ark-base-url' {...form.register('base_url')} />
        </Field>
        <NumberField
          form={form}
          name='timeout_seconds'
          label={t('models.providerFields.timeoutLabel')}
        />
        <NumberField
          form={form}
          name='retry_times'
          label={t('models.providerFields.retryTimesLabel')}
        />
        {replaceCredentials && authMode === 'api_key' && (
          <Field label='API Key' htmlFor='ark-api-key'>
            <Input
              id='ark-api-key'
              type='password'
              {...form.register('api_key')}
            />
          </Field>
        )}
        {replaceCredentials && authMode === 'ak_sk' && (
          <>
            <Field label='Access Key' htmlFor='ark-access-key'>
              <Input
                id='ark-access-key'
                type='password'
                {...form.register('access_key')}
              />
            </Field>
            <Field label='Secret Key' htmlFor='ark-secret-key'>
              <Input
                id='ark-secret-key'
                type='password'
                {...form.register('secret_key')}
              />
            </Field>
          </>
        )}
      </div>
    )
  }
  if (provider === 'ollama') {
    return (
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field label='Base URL' htmlFor='ollama-base-url'>
          <Input id='ollama-base-url' {...form.register('base_url')} />
        </Field>
        <NumberField
          form={form}
          name='timeout_seconds'
          label={t('models.providerFields.timeoutLabel')}
        />
      </div>
    )
  }
  if (provider === 'dashscope') {
    return (
      <div className='grid gap-4 sm:grid-cols-2'>
        <NumberField
          form={form}
          name='timeout_seconds'
          label={t('models.providerFields.timeoutLabel')}
        />
        {replaceCredentials && (
          <Field label='API Key' htmlFor='dashscope-api-key'>
            <Input
              id='dashscope-api-key'
              type='password'
              {...form.register('api_key')}
            />
          </Field>
        )}
      </div>
    )
  }
  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <Field label='Region' htmlFor='tencent-region'>
        <Input id='tencent-region' {...form.register('region')} />
      </Field>
      {replaceCredentials && (
        <>
          <Field label='Secret ID' htmlFor='tencent-secret-id'>
            <Input
              id='tencent-secret-id'
              type='password'
              {...form.register('secret_id')}
            />
          </Field>
          <Field label='Secret Key' htmlFor='tencent-secret-key'>
            <Input
              id='tencent-secret-key'
              type='password'
              {...form.register('secret_key')}
            />
          </Field>
        </>
      )}
    </div>
  )
}

function NumberField({
  form,
  name,
  label,
}: {
  form: ReturnType<typeof useForm<ProviderFormValues>>
  name: 'timeout_seconds' | 'retry_times'
  label: string
}) {
  const id = `provider-${name}`
  return (
    <Field label={label} htmlFor={id}>
      <Input
        id={id}
        type='number'
        {...form.register(name, { valueAsNumber: true })}
      />
    </Field>
  )
}

export function Field({
  label,
  htmlFor,
  children,
}: {
  label: string
  htmlFor: string
  children: React.ReactNode
}) {
  return (
    <div className='grid gap-2'>
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  )
}
