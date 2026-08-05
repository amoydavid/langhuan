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
import { type RerankModelFormValues, rerankModelFormSchema } from '../schemas'
import {
  type Model,
  type ModelProvider,
  type ModelScope,
  rerankModelFormDefaults,
} from '../types'
import { RerankModelFields } from './rerank-model-fields'

type RerankModelFormProps = {
  provider: ModelProvider
  scope: ModelScope
  workspaceSlug?: string
  model?: Model
  onSaved?: (model: Model) => void
}

function rerankNumberParameter(
  model: Model | undefined,
  key: string,
  fallback: number
) {
  const value = model?.parameters[key]
  return typeof value === 'number' ? value : fallback
}

export function RerankModelForm({
  provider,
  scope,
  workspaceSlug,
  model,
  onSaved,
}: RerankModelFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<RerankModelFormValues>({
    resolver: zodResolver(rerankModelFormSchema),
    defaultValues: {
      ...rerankModelFormDefaults,
      name: model?.name ?? '',
      display_name: model?.display_name ?? '',
      description: model?.description ?? '',
      model_name: model?.model_name ?? '',
      max_documents: rerankNumberParameter(model, 'max_documents', 100),
      max_query_chars: rerankNumberParameter(model, 'max_query_chars', 4096),
      max_document_chars: rerankNumberParameter(
        model,
        'max_document_chars',
        8192
      ),
    },
  })
  const mutation = useMutation({
    mutationFn: (values: RerankModelFormValues) => {
      const input = {
        display_name: values.display_name,
        description: values.description,
        model_name: values.model_name,
        parameters: {
          max_documents: values.max_documents,
          max_query_chars: values.max_query_chars,
          max_document_chars: values.max_document_chars,
        },
      }
      return model
        ? updateModel(scope, model.id, input, workspaceSlug)
        : createModel(
            scope,
            provider.id,
            { ...input, name: values.name, type: 'rerank' },
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
          htmlFor='rerank-model-name'
          error={errors.name?.message}
        >
          <Input
            id='rerank-model-name'
            disabled={model !== undefined}
            aria-invalid={Boolean(errors.name)}
            {...form.register('name')}
          />
        </Field>
        <Field
          label={t('models.modelForm.displayNameLabel')}
          htmlFor='rerank-model-display-name'
          error={errors.display_name?.message}
        >
          <Input
            id='rerank-model-display-name'
            aria-invalid={Boolean(errors.display_name)}
            {...form.register('display_name')}
          />
        </Field>
        <Field
          label={t('models.modelForm.modelNameLabel')}
          htmlFor='rerank-model-upstream-name'
          error={errors.model_name?.message}
        >
          <Input
            id='rerank-model-upstream-name'
            aria-invalid={Boolean(errors.model_name)}
            {...form.register('model_name')}
          />
        </Field>
        <RerankModelFields register={form.register} errors={errors} />
      </div>
      <Field
        label={t('models.modelForm.descriptionLabel')}
        htmlFor='rerank-model-description'
        error={errors.description?.message}
      >
        <Textarea
          id='rerank-model-description'
          className='min-h-20'
          aria-invalid={Boolean(errors.description)}
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
