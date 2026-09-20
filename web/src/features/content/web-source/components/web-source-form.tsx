import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Save } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { parseApiError } from '@/lib/api/error'
import { readWebSourceConfig } from '../config'
import {
  buildCreatePayload,
  buildUpdatePayload,
  type WebSourceFormValues,
  webSourceCreateDefaults,
  webSourceEditDefaults,
  webSourceFormSchema,
} from '../form-schema'
import {
  createWebSourceMutationOptions,
  updateWebSourceMutationOptions,
} from '../queries'
import type { WebSourceView } from '../types'
import { WebSourceAdvancedOptions } from './web-source-form-advanced'
import { ReadOnlyField } from './web-source-form-fields'

const modeOptions = ['crawl', 'sitemap', 'urls'] as const

const modeDescKeys = {
  crawl: 'webSource.form.modeCrawlDesc',
  sitemap: 'webSource.form.modeSitemapDesc',
  urls: 'webSource.form.modeUrlsDesc',
} as const

type WebSourceFormProps = {
  workspaceSlug: string
  kbId: string
  /** 传入即为编辑态；缺省为创建。 */
  source?: WebSourceView
  onDone: () => void
}

/** 添加/编辑网站源同一张表单（spec §9.5.3/§9.5.4）；编辑态不可变项只读展示。 */
export function WebSourceForm({
  workspaceSlug,
  kbId,
  source,
  onDone,
}: WebSourceFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isEdit = source !== undefined

  const form = useForm<WebSourceFormValues>({
    resolver: zodResolver(webSourceFormSchema),
    defaultValues: source
      ? webSourceEditDefaults(
          source.source.name,
          readWebSourceConfig(source.source.config)
        )
      : webSourceCreateDefaults(),
  })
  const mode = form.watch('mode')

  const createMutation = useMutation(
    createWebSourceMutationOptions(workspaceSlug, kbId, queryClient)
  )
  const updateMutation = useMutation(
    updateWebSourceMutationOptions(
      workspaceSlug,
      kbId,
      source?.source.id ?? '',
      queryClient
    )
  )
  const mutation = isEdit ? updateMutation : createMutation

  async function onSubmit(values: WebSourceFormValues) {
    try {
      const result = isEdit
        ? await updateMutation.mutateAsync(buildUpdatePayload(values))
        : await createMutation.mutateAsync(buildCreatePayload(values))
      toast.success(
        t(isEdit ? 'webSource.form.savedToast' : 'webSource.form.createdToast'),
        { description: result.job_id }
      )
      onDone()
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className='grid gap-5'
        aria-label={
          isEdit
            ? t('webSource.form.editTitle', { name: source.source.name })
            : t('webSource.form.createTitle')
        }
      >
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('webSource.form.nameLabel')}</FormLabel>
              <FormControl>
                <Input
                  autoFocus
                  placeholder={t('webSource.form.namePlaceholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {isEdit ? (
          <FormItem>
            <FormLabel>{t('webSource.form.modeLabel')}</FormLabel>
            <FormControl>
              <div className='rounded-lg border px-3 py-2 text-muted-foreground text-sm'>
                {t(`webSource.mode.${mode}`)}
              </div>
            </FormControl>
            <FormDescription>{t('webSource.form.engineHint')}</FormDescription>
          </FormItem>
        ) : (
          <FormField
            control={form.control}
            name='mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('webSource.form.modeLabel')}</FormLabel>
                <FormControl>
                  <RadioGroup
                    value={field.value}
                    onValueChange={field.onChange}
                    className='grid gap-2 sm:grid-cols-3'
                  >
                    {modeOptions.map((value) => (
                      // 原生 label htmlFor → item id：整卡（含文字与描述）都是
                      // radio 的点击区域，且 radio 获得可访问名称。不要用
                      // FormControl 包卡片 label——Radix slot 会把字段 id 注入
                      // label（for 指向 radiogroup 容器），导致点文字无效、
                      // 只有圆点可点（2026-09-18 用户实测）。
                      <label
                        key={value}
                        htmlFor={`web-source-mode-${value}`}
                        className='flex cursor-pointer flex-col gap-1 rounded-lg border px-3 py-2 font-normal has-[button[data-state=checked]]:border-primary'
                      >
                        <span className='flex items-center gap-2'>
                          <RadioGroupItem
                            value={value}
                            id={`web-source-mode-${value}`}
                          />
                          {t(`webSource.mode.${value}`)}
                        </span>
                        <span className='text-muted-foreground text-xs'>
                          {t(modeDescKeys[value])}
                        </span>
                      </label>
                    ))}
                  </RadioGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        {mode === 'crawl' &&
          (isEdit ? (
            <ReadOnlyField
              label={t('webSource.form.seedUrlLabel')}
              value={form.getValues('seed_url')}
            />
          ) : (
            <FormField
              control={form.control}
              name='seed_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('webSource.form.seedUrlLabel')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://docs.example.com/'
                      autoComplete='off'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('webSource.form.seedUrlHint')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}

        {mode === 'sitemap' &&
          (isEdit ? (
            <ReadOnlyField
              label={t('webSource.form.sitemapUrlLabel')}
              value={form.getValues('sitemap_url')}
            />
          ) : (
            <FormField
              control={form.control}
              name='sitemap_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('webSource.form.sitemapUrlLabel')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://example.com/sitemap.xml'
                      autoComplete='off'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('webSource.form.sitemapUrlHint')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}

        {mode === 'urls' && !isEdit && (
          <FormField
            control={form.control}
            name='urls_text'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('webSource.form.urlsLabel')}</FormLabel>
                <FormControl>
                  <Textarea
                    className='min-h-28 font-mono text-xs'
                    placeholder={t('webSource.form.urlsPlaceholder')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('webSource.form.urlsHint')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}
        {mode === 'urls' && isEdit && (
          <Alert>
            <AlertDescription>
              {t('webSource.form.urlsEditHint')}
            </AlertDescription>
          </Alert>
        )}

        <WebSourceAdvancedOptions
          form={form}
          workspaceSlug={workspaceSlug}
          isEdit={isEdit}
        />

        <div className='flex items-center gap-2'>
          <Button type='submit' disabled={mutation.isPending}>
            {mutation.isPending ? (
              <Loader2 className='animate-spin' />
            ) : isEdit ? (
              <Save />
            ) : (
              <Plus />
            )}
            {t(
              isEdit
                ? 'webSource.form.submitEdit'
                : 'webSource.form.submitCreate'
            )}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={onDone}
          >
            {t('webSource.form.cancel')}
          </Button>
        </div>
      </form>
    </Form>
  )
}
