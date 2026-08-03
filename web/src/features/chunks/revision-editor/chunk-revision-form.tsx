import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Textarea } from '@/components/ui/textarea'
import type {
  Chunk,
  ChunkRevision,
  CreateChunkRevisionInput,
} from '@/features/chunks/types'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'

const chunkRevisionFormSchema = z
  .object({
    context_header: z
      .string()
      .trim()
      .max(500, i18n.t('chunks.revisionForm.schemas.contextHeaderTooLong')),
    content: z.string(),
    enabled: z.boolean(),
  })
  .superRefine((values, context) => {
    if (values.enabled && values.content.trim().length === 0) {
      context.addIssue({
        code: 'custom',
        message: i18n.t(
          'chunks.revisionForm.schemas.contentRequiredWhenEnabled'
        ),
        path: ['content'],
      })
    }
  })

type ChunkRevisionFormProps = {
  chunk: Chunk
  latestRevision?: ChunkRevision
  saveRevision: (input: CreateChunkRevisionInput) => Promise<ChunkRevision>
  onSaved?: (revision: ChunkRevision) => void
  onCancel?: () => void
  onFAQRedirect?: () => void
  onDirtyChange?: (dirty: boolean) => void
}

export function ChunkRevisionForm({
  chunk,
  latestRevision,
  saveRevision,
  onSaved,
  onCancel,
  onFAQRedirect,
  onDirtyChange,
}: ChunkRevisionFormProps) {
  const { t } = useTranslation()
  const active = chunk.active_revision
  const [submitError, setSubmitError] =
    useState<ReturnType<typeof parseApiError>>()
  const [baseRevisionId, setBaseRevisionId] = useState(active?.id ?? '')
  const form = useForm<z.infer<typeof chunkRevisionFormSchema>>({
    resolver: zodResolver(chunkRevisionFormSchema),
    defaultValues: {
      context_header: active?.context_header ?? '',
      content: active?.content ?? '',
      enabled: active?.enabled ?? true,
    },
  })

  useEffect(() => {
    function preventDirtyLeave(event: BeforeUnloadEvent) {
      if (!form.formState.isDirty) return
      event.preventDefault()
    }
    window.addEventListener('beforeunload', preventDirtyLeave)
    return () => window.removeEventListener('beforeunload', preventDirtyLeave)
  }, [form.formState.isDirty])

  useEffect(() => {
    onDirtyChange?.(form.formState.isDirty)
  }, [form.formState.isDirty, onDirtyChange])

  if (!active) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('chunks.revisionForm.noActiveRevision')}
      </p>
    )
  }
  async function submitWithBase(
    values: z.infer<typeof chunkRevisionFormSchema>,
    revisionId: string
  ) {
    setSubmitError(undefined)
    try {
      const saved = await saveRevision({
        base_revision_id: revisionId,
        context_header: values.context_header.trim(),
        content: values.content,
        enabled: values.enabled,
      })
      form.reset(values)
      onSaved?.(saved)
    } catch (error) {
      const apiError = parseApiError(error)
      setSubmitError(apiError)
      if (apiError.code === 'faq_chunk_immutable') onFAQRedirect?.()
    }
  }

  async function submit(values: z.infer<typeof chunkRevisionFormSchema>) {
    await submitWithBase(values, baseRevisionId)
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(submit)} className='space-y-5'>
        {submitError && (
          <Alert variant='destructive' tabIndex={-1}>
            <AlertTitle>
              {submitError.code === 'revision_conflict'
                ? t('chunks.revisionForm.conflictTitle')
                : t('chunks.revisionForm.saveFailedTitle')}
            </AlertTitle>
            <AlertDescription>{submitError.message}</AlertDescription>
          </Alert>
        )}

        {submitError?.code === 'revision_conflict' && latestRevision && (
          <div className='space-y-3 rounded-xl border p-4'>
            <div className='grid gap-3 md:grid-cols-2'>
              <div>
                <h3 className='font-medium text-sm'>
                  {t('chunks.revisionForm.yourVersionTitle')}
                </h3>
                <pre className='mt-2 whitespace-pre-wrap rounded-lg bg-muted/40 p-3 text-sm'>
                  {form.getValues('content')}
                </pre>
              </div>
              <div>
                <h3 className='font-medium text-sm'>
                  {t('chunks.revisionForm.latestVersionTitle')}
                </h3>
                <pre className='mt-2 whitespace-pre-wrap rounded-lg bg-muted/40 p-3 text-sm'>
                  {latestRevision.content}
                </pre>
              </div>
            </div>
            <div className='flex justify-end'>
              <Button
                type='button'
                variant='outline'
                onClick={async () => {
                  const valid = await form.trigger()
                  if (!valid) return
                  setBaseRevisionId(latestRevision.id)
                  await submitWithBase(form.getValues(), latestRevision.id)
                }}
              >
                {t('chunks.revisionForm.retryWithLatest')}
              </Button>
            </div>
          </div>
        )}

        <FormField
          control={form.control}
          name='context_header'
          render={({ field }) => (
            <FormItem>
              <FormLabel>Context header</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormDescription>
                {t('chunks.revisionForm.contextHeaderDescription')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='content'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('chunks.revisionForm.contentLabel')}</FormLabel>
              <FormControl>
                <Textarea {...field} rows={10} />
              </FormControl>
              <FormDescription>
                {t('chunks.revisionForm.contentDescription')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem className='flex flex-row items-start gap-3 rounded-lg border p-4'>
              <FormControl>
                <Checkbox
                  checked={field.value}
                  onCheckedChange={(value) => field.onChange(value === true)}
                />
              </FormControl>
              <div className='space-y-1'>
                <FormLabel>{t('chunks.revisionForm.enabledLabel')}</FormLabel>
                <FormDescription>
                  {t('chunks.revisionForm.enabledDescription')}
                </FormDescription>
              </div>
            </FormItem>
          )}
        />
        <div className='flex justify-end gap-2'>
          {onCancel && (
            <Button type='button' variant='outline' onClick={onCancel}>
              {t('chunks.revisionForm.cancelButton')}
            </Button>
          )}
          <Button type='submit' disabled={form.formState.isSubmitting}>
            {form.formState.isSubmitting && (
              <Loader2 className='animate-spin' />
            )}
            {t('chunks.revisionForm.saveButton')}
          </Button>
        </div>
      </form>
    </Form>
  )
}
