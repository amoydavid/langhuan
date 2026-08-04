import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { FileUp, Loader2 } from 'lucide-react'
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
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'
import { uploadDocument } from '../api'
import { type DocumentUploadFormValues, documentUploadSchema } from '../schemas'

export const DOCUMENT_ACCEPT = '.pdf,.md,.markdown,.txt,.csv,.xlsx,.docx'

export function documentUploadErrorMessage(error: unknown) {
  const apiError = parseApiError(error)
  if (apiError.status === 413) {
    return i18n.t('documents.uploadForm.uploadSizeLimitError')
  }
  if (apiError.status === 415 && apiError.code === 'unsupported_file_type') {
    return i18n.t('documents.uploadForm.unsupportedTypeError')
  }
  return apiError.message
}

type DocumentUploadFormProps = {
  workspaceSlug: string
  kbId: string
  parentNodeId?: string
}

export function DocumentUploadForm({
  workspaceSlug,
  kbId,
  parentNodeId,
}: DocumentUploadFormProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  const form = useForm<DocumentUploadFormValues>({
    resolver: zodResolver(documentUploadSchema),
    defaultValues: {
      title: '',
      source_type: 'upload',
      dedupe: true,
    },
  })
  const mutation = useMutation({
    mutationFn: (values: DocumentUploadFormValues) =>
      uploadDocument(workspaceSlug, kbId, {
        ...values,
        parent_node_id: parentNodeId,
        node_name: values.file.name,
      }),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({
        queryKey: ['documents', workspaceSlug, kbId],
      })
      await queryClient.invalidateQueries({
        queryKey: ['file-tree', workspaceSlug, kbId],
      })
      toast.success(
        result.deduped
          ? t('documents.uploadForm.dedupedToast')
          : t('documents.uploadForm.uploadedToast')
      )
      await navigate({
        to: '/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId',
        params: {
          workspaceSlug,
          kbId,
          documentId: result.document.id,
        },
        search: { job: result.job.id },
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
          name='file'
          render={({ field: { onChange, value: _value, ...field } }) => (
            <FormItem>
              <FormLabel>{t('documents.uploadForm.fileLabel')}</FormLabel>
              <FormControl>
                <Input
                  type='file'
                  accept={DOCUMENT_ACCEPT}
                  {...field}
                  onChange={(event) => {
                    const file = event.target.files?.[0]
                    onChange(file)
                    if (file) {
                      form.setValue('title', file.name, {
                        shouldDirty: true,
                        shouldValidate: true,
                      })
                    }
                  }}
                />
              </FormControl>
              <FormDescription>
                {t('documents.uploadForm.fileDescription')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='title'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('documents.uploadForm.titleLabel')}</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormDescription>
                {t('documents.uploadForm.titleDescription')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='source_type'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('documents.uploadForm.sourceTypeLabel')}</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='dedupe'
          render={({ field }) => (
            <FormItem className='flex flex-row items-start gap-3 rounded-lg border p-4'>
              <FormControl>
                <Checkbox
                  checked={field.value}
                  onCheckedChange={(checked) =>
                    field.onChange(checked === true)
                  }
                />
              </FormControl>
              <div className='space-y-1 leading-none'>
                <FormLabel>{t('documents.uploadForm.dedupeLabel')}</FormLabel>
                <FormDescription>
                  {t('documents.uploadForm.dedupeDescription')}
                </FormDescription>
              </div>
            </FormItem>
          )}
        />
        {mutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {documentUploadErrorMessage(mutation.error)}
          </p>
        )}
        <Button className='w-fit' disabled={mutation.isPending}>
          {mutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <FileUp />
          )}
          {t('documents.uploadForm.submit')}
        </Button>
      </form>
    </Form>
  )
}
