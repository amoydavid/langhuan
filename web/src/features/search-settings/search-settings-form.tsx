import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, Save } from 'lucide-react'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { Model } from '@/features/models/types'
import { parseApiError } from '@/lib/api/error'
import type { WorkspaceSearchSettings } from './types'

export const searchSettingsFormSchema = z
  .object({
    rerank_enabled: z.boolean(),
    rerank_model_id: z.string().optional(),
    candidate_top_k: z.number().int().min(50).max(200),
    failure_mode: z.enum(['fallback', 'fail']),
  })
  .superRefine((values, ctx) => {
    if (values.rerank_enabled && !values.rerank_model_id) {
      ctx.addIssue({
        code: 'custom',
        path: ['rerank_model_id'],
        message: '请选择 Rerank 模型',
      })
    }
  })

export type SearchSettingsFormValues = z.infer<typeof searchSettingsFormSchema>

type SearchSettingsFormProps = {
  settings: WorkspaceSearchSettings
  models: Model[]
  save: (values: SearchSettingsFormValues) => Promise<void>
}

export function SearchSettingsForm({
  settings,
  models,
  save,
}: SearchSettingsFormProps) {
  const { t } = useTranslation()
  const form = useForm<SearchSettingsFormValues>({
    resolver: zodResolver(searchSettingsFormSchema),
    defaultValues: {
      rerank_enabled: settings.rerank !== null,
      rerank_model_id: settings.rerank?.model_id,
      candidate_top_k: settings.rerank?.candidate_top_k ?? 50,
      failure_mode: settings.rerank?.failure_mode ?? 'fallback',
    },
  })
  const enabled = form.watch('rerank_enabled')

  useEffect(() => {
    form.reset({
      rerank_enabled: settings.rerank !== null,
      rerank_model_id: settings.rerank?.model_id,
      candidate_top_k: settings.rerank?.candidate_top_k ?? 50,
      failure_mode: settings.rerank?.failure_mode ?? 'fallback',
    })
  }, [form, settings])

  async function submit(values: SearchSettingsFormValues) {
    try {
      await save(values)
    } catch (error) {
      form.setError('root', { message: parseApiError(error).message })
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(submit)} className='space-y-5'>
        <FormField
          control={form.control}
          name='rerank_enabled'
          render={({ field }) => (
            <FormItem className='flex min-h-11 items-center justify-between rounded-lg border px-3'>
              <div>
                <FormLabel>{t('searchSettings.form.enabledLabel')}</FormLabel>
                <FormDescription>
                  {t('searchSettings.form.enabledDescription')}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
        {enabled && (
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='rerank_model_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('searchSettings.form.modelLabel')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue
                          placeholder={t(
                            'searchSettings.form.modelPlaceholder'
                          )}
                        />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {models.map((model) => (
                        <SelectItem key={model.id} value={model.id}>
                          {model.display_name} · {model.provider.display_name} ·{' '}
                          {model.model_name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t('searchSettings.form.modelDescription')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='candidate_top_k'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('searchSettings.form.candidateLabel')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={50}
                      max={200}
                      {...field}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('searchSettings.form.candidateDescription')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='failure_mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('searchSettings.form.failureLabel')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value='fallback'>
                        {t('searchSettings.form.failureFallback')}
                      </SelectItem>
                      <SelectItem value='fail'>
                        {t('searchSettings.form.failureFail')}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t('searchSettings.form.failureDescription')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        )}
        <p className='text-muted-foreground text-sm'>
          {t('searchSettings.form.scopeDescription')}
        </p>
        {form.formState.errors.root?.message && (
          <p role='alert' className='text-destructive text-sm'>
            {form.formState.errors.root.message}
          </p>
        )}
        <Button type='submit' disabled={form.formState.isSubmitting}>
          {form.formState.isSubmitting ? (
            <Loader2 className='animate-spin' />
          ) : (
            <Save />
          )}
          {t('searchSettings.form.save')}
        </Button>
      </form>
    </Form>
  )
}
