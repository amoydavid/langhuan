import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Database, Loader2 } from 'lucide-react'
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
import { Textarea } from '@/components/ui/textarea'
import type { Role } from '@/features/auth/types'
import { selectableModelsQueryOptions } from '@/features/models/queries'
import { parseApiError } from '@/lib/api/error'
import { createKnowledgeBase } from '../api'
import { type KnowledgeBaseFormValues, knowledgeBaseSchema } from '../schemas'
import { EmbeddingModelSelect } from './embedding-model-select'

type KnowledgeBaseFormProps = {
  workspaceSlug: string
  workspaceRole: Role
}

export function KnowledgeBaseForm({
  workspaceSlug,
  workspaceRole,
}: KnowledgeBaseFormProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  const { data: models = [], isPending: modelsPending } = useQuery(
    selectableModelsQueryOptions(workspaceSlug)
  )
  const form = useForm<KnowledgeBaseFormValues>({
    resolver: zodResolver(knowledgeBaseSchema),
    defaultValues: {
      name: '',
      description: '',
      embedding_model_id: '',
      chunk_size: 512,
      chunk_overlap: 80,
    },
  })
  const mutation = useMutation({
    mutationFn: (values: KnowledgeBaseFormValues) =>
      createKnowledgeBase(workspaceSlug, {
        name: values.name,
        description: values.description,
        embedding_model_id: values.embedding_model_id,
        chunking_config: {
          chunk_size: values.chunk_size,
          chunk_overlap: values.chunk_overlap,
        },
      }),
    onSuccess: async (knowledgeBase) => {
      await queryClient.invalidateQueries({
        queryKey: ['knowledge-bases', workspaceSlug],
      })
      toast.success(t('knowledgeBases.form.createdToast'))
      await navigate({
        to: `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(knowledgeBase.id)}`,
        replace: true,
      })
    },
  })

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        className='grid gap-5'
      >
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('knowledgeBases.form.nameLabel')}</FormLabel>
              <FormControl>
                <Input autoFocus {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='description'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('knowledgeBases.form.descriptionLabel')}</FormLabel>
              <FormControl>
                <Textarea className='min-h-24' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='embedding_model_id'
          render={({ field, fieldState }) => (
            <EmbeddingModelSelect
              workspaceSlug={workspaceSlug}
              workspaceRole={workspaceRole}
              models={models}
              value={field.value}
              disabled={modelsPending}
              onChange={(modelId) =>
                form.setValue('embedding_model_id', modelId, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              error={fieldState.error?.message}
            />
          )}
        />
        <div className='grid gap-5 sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='chunk_size'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('knowledgeBases.form.chunkSizeLabel')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    step={1}
                    {...field}
                    onChange={(event) =>
                      field.onChange(event.target.valueAsNumber)
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='chunk_overlap'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('knowledgeBases.form.chunkOverlapLabel')}
                </FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    step={1}
                    {...field}
                    onChange={(event) =>
                      field.onChange(event.target.valueAsNumber)
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
        {mutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(mutation.error).message}
          </p>
        )}
        <Button
          className='w-fit'
          disabled={mutation.isPending || modelsPending || models.length === 0}
        >
          {mutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <Database />
          )}
          {t('knowledgeBases.form.submitButton')}
        </Button>
      </form>
    </Form>
  )
}
