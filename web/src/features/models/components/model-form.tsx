import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Boxes, Loader2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { parseApiError } from '@/lib/api/error'
import { createModel, updateModel } from '../api'
import { invalidateSelectableModels } from '../cache'
import { type ModelFormValues, modelFormSchema } from '../schemas'
import {
  embeddingDimensions,
  type Model,
  type ModelProvider,
  type ModelScope,
  modelFormDefaults,
} from '../types'
import { RerankModelForm } from './rerank-model-form'

type ModelFormProps = {
  provider: ModelProvider
  scope: ModelScope
  workspaceSlug?: string
  model?: Model
  onSaved?: (model: Model) => void
}

export function ModelForm(props: ModelFormProps) {
  if (props.provider.provider === 'rerank_compatible') {
    return <RerankModelForm {...props} />
  }
  return <EmbeddingModelForm {...props} />
}

function numberParameter(
  model: Model | undefined,
  key: string,
  fallback: number
) {
  const value = model?.parameters[key]
  return typeof value === 'number' ? value : fallback
}

export function EmbeddingModelForm({
  provider,
  scope,
  workspaceSlug,
  model,
  onSaved,
}: ModelFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const fixed1024 =
    provider.provider === 'dashscope' || provider.provider === 'tencentcloud'
  const fixedTencentModel = provider.provider === 'tencentcloud'
  const form = useForm<ModelFormValues>({
    resolver: zodResolver(modelFormSchema),
    defaultValues: {
      ...modelFormDefaults,
      name: model?.name ?? '',
      display_name: model?.display_name ?? '',
      description: model?.description ?? '',
      model_name: fixedTencentModel
        ? 'hunyuan-embedding'
        : (model?.model_name ?? ''),
      dimensions: fixed1024 ? 1024 : (model?.dimensions ?? 1024),
      batch_size: numberParameter(model, 'batch_size', 32),
      truncate:
        typeof model?.parameters.truncate === 'boolean'
          ? model.parameters.truncate
          : false,
      keep_alive_seconds:
        typeof model?.parameters.keep_alive_seconds === 'number'
          ? model.parameters.keep_alive_seconds
          : undefined,
    },
  })
  const mutation = useMutation({
    mutationFn: (values: ModelFormValues) => {
      const parameters =
        provider.provider === 'tencentcloud'
          ? {}
          : provider.provider === 'ollama'
            ? {
                batch_size: values.batch_size,
                truncate: values.truncate,
                ...(values.keep_alive_seconds === undefined
                  ? {}
                  : { keep_alive_seconds: values.keep_alive_seconds }),
              }
            : { batch_size: values.batch_size }
      const input = {
        display_name: values.display_name,
        description: values.description,
        model_name: values.model_name,
        dimensions: values.dimensions,
        parameters,
      }
      return model
        ? updateModel(scope, model.id, input, workspaceSlug)
        : createModel(
            scope,
            provider.id,
            { ...input, name: values.name, type: 'embedding' },
            workspaceSlug
          )
    },
    onSuccess: async (saved) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['models', scope, workspaceSlug ?? null, provider.id],
        }),
        invalidateSelectableModels(queryClient, scope, workspaceSlug),
      ])
      toast.success(
        model
          ? t('models.modelForm.updatedToast')
          : t('models.modelForm.addedToast')
      )
      onSaved?.(saved)
    },
  })
  const errors = form.formState.errors

  return (
    <form
      className='grid gap-5'
      onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
    >
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field
          label={t('models.modelForm.nameLabel')}
          htmlFor='model-name'
          error={errors.name?.message}
        >
          <Input
            id='model-name'
            disabled={model !== undefined}
            aria-invalid={Boolean(errors.name)}
            aria-describedby={errors.name ? 'model-name-error' : undefined}
            {...form.register('name')}
          />
        </Field>
        <Field
          label={t('models.modelForm.displayNameLabel')}
          htmlFor='model-display-name'
          error={errors.display_name?.message}
        >
          <Input
            id='model-display-name'
            aria-invalid={Boolean(errors.display_name)}
            aria-describedby={
              errors.display_name ? 'model-display-name-error' : undefined
            }
            {...form.register('display_name')}
          />
        </Field>
        <Field
          label={t('models.modelForm.modelNameLabel')}
          htmlFor='model-upstream-name'
          error={errors.model_name?.message}
        >
          <Input
            id='model-upstream-name'
            disabled={fixedTencentModel}
            aria-invalid={Boolean(errors.model_name)}
            aria-describedby={
              errors.model_name ? 'model-upstream-name-error' : undefined
            }
            {...form.register('model_name')}
          />
        </Field>
        <Field
          label={t('models.modelForm.dimensionsLabel')}
          htmlFor='model-dimensions'
          error={errors.dimensions?.message}
        >
          <select
            id='model-dimensions'
            className='h-9 rounded-md border bg-background px-3 text-sm disabled:opacity-60'
            disabled={fixed1024}
            aria-invalid={Boolean(errors.dimensions)}
            aria-describedby={
              errors.dimensions ? 'model-dimensions-error' : undefined
            }
            {...form.register('dimensions', { valueAsNumber: true })}
          >
            {(fixed1024 ? [1024] : embeddingDimensions).map((dimension) => (
              <option key={dimension} value={dimension}>
                {dimension}
              </option>
            ))}
          </select>
        </Field>
        {provider.provider !== 'tencentcloud' && (
          <Field
            label={t('models.modelForm.batchSizeLabel')}
            htmlFor='model-batch-size'
            error={errors.batch_size?.message}
          >
            <Input
              id='model-batch-size'
              type='number'
              min={1}
              max={200}
              aria-invalid={Boolean(errors.batch_size)}
              aria-describedby={
                errors.batch_size ? 'model-batch-size-error' : undefined
              }
              {...form.register('batch_size', { valueAsNumber: true })}
            />
          </Field>
        )}
        {provider.provider === 'ollama' && (
          <>
            <Field
              label={t('models.modelForm.truncateLabel')}
              htmlFor='model-truncate'
              error={errors.truncate?.message}
            >
              <input
                id='model-truncate'
                type='checkbox'
                className='size-4'
                aria-invalid={Boolean(errors.truncate)}
                aria-describedby={
                  errors.truncate ? 'model-truncate-error' : undefined
                }
                {...form.register('truncate')}
              />
            </Field>
            <Field
              label={t('models.modelForm.keepAliveLabel')}
              htmlFor='model-keep-alive'
              error={errors.keep_alive_seconds?.message}
            >
              <Input
                id='model-keep-alive'
                type='number'
                min={0}
                max={86400}
                aria-invalid={Boolean(errors.keep_alive_seconds)}
                aria-describedby={
                  errors.keep_alive_seconds
                    ? 'model-keep-alive-error'
                    : undefined
                }
                {...form.register('keep_alive_seconds', {
                  setValueAs: (value: string) =>
                    value === '' ? undefined : Number(value),
                })}
              />
            </Field>
          </>
        )}
      </div>
      <Field
        label={t('models.modelForm.descriptionLabel')}
        htmlFor='model-description'
        error={errors.description?.message}
      >
        <Textarea
          id='model-description'
          className='min-h-20'
          aria-invalid={Boolean(errors.description)}
          aria-describedby={
            errors.description ? 'model-description-error' : undefined
          }
          {...form.register('description')}
        />
      </Field>
      {mutation.isError && (
        <p className='text-destructive text-sm' role='alert'>
          {parseApiError(mutation.error).message}
        </p>
      )}
      <Button className='w-fit' disabled={mutation.isPending}>
        {mutation.isPending ? <Loader2 className='animate-spin' /> : <Boxes />}
        {t('models.modelForm.submitButton')}
      </Button>
    </form>
  )
}

function Field({
  label,
  htmlFor,
  error,
  children,
}: {
  label: string
  htmlFor: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className='grid gap-2'>
      <Label
        htmlFor={htmlFor}
        className={error ? 'text-destructive' : undefined}
      >
        {label}
      </Label>
      {children}
      {error && (
        <p
          id={`${htmlFor}-error`}
          className='text-destructive text-sm'
          role='alert'
        >
          {error}
        </p>
      )}
    </div>
  )
}
