import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Database, Loader2 } from 'lucide-react'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type { Role } from '@/features/auth/types'
import { sourceConnectionsQueryOptions } from '@/features/integrations/queries'
import { selectableModelsQueryOptions } from '@/features/models/queries'
import { parseApiError } from '@/lib/api/error'
import { createKnowledgeBase } from '../api'
import { type KnowledgeBaseFormValues, knowledgeBaseSchema } from '../schemas'
import type { CreateKnowledgeBaseInput } from '../types'
import { EmbeddingModelSelect } from './embedding-model-select'

type KnowledgeBaseFormProps = {
  workspaceSlug: string
  workspaceRole: Role
}

// 根据 source_type 推断飞书根节点类型：drive → 文件夹，wiki → 知识库节点。
function rootKindFor(sourceType: KnowledgeBaseFormValues['source_type']) {
  return sourceType === 'feishu_drive' ? 'drive_folder' : 'wiki_node'
}

// 将表单值打包成后端期望的创建输入；upload 时与历史行为完全一致。
function buildCreateInput(
  values: KnowledgeBaseFormValues
): CreateKnowledgeBaseInput {
  const input: CreateKnowledgeBaseInput = {
    name: values.name,
    description: values.description,
    embedding_model_id: values.embedding_model_id,
    chunking_config: {
      strategy: values.strategy,
      enable_parent_child: values.enable_parent_child,
      parent_chunk_size: values.parent_chunk_size,
      child_chunk_size: values.child_chunk_size,
      chunk_size: values.chunk_size,
      chunk_overlap: values.chunk_overlap,
    },
  }
  if (values.source_type !== 'upload') {
    input.source_type = values.source_type
    input.source_connection_id = values.source_connection_id
    input.source_config = {
      root_token: values.root_token,
      root_kind: rootKindFor(values.source_type),
      ...(values.sync_enabled && values.cron ? { cron: values.cron } : {}),
    }
  }
  return input
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
  const { data: connections = [] } = useQuery(
    sourceConnectionsQueryOptions(workspaceSlug)
  )
  const form = useForm<KnowledgeBaseFormValues>({
    resolver: zodResolver(knowledgeBaseSchema),
    defaultValues: {
      name: '',
      description: '',
      embedding_model_id: '',
      strategy: 'auto',
      enable_parent_child: true,
      parent_chunk_size: 4096,
      child_chunk_size: 384,
      chunk_size: 512,
      chunk_overlap: 80,
      source_type: 'upload',
      source_connection_id: undefined,
      root_token: '',
      sync_enabled: false,
      cron: '',
    },
  })
  const sourceType = form.watch('source_type')
  const syncEnabled = form.watch('sync_enabled')
  const isFeishu = sourceType !== 'upload'
  const mutation = useMutation({
    mutationFn: (values: KnowledgeBaseFormValues) =>
      createKnowledgeBase(workspaceSlug, buildCreateInput(values)),
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
        <FormField
          control={form.control}
          name='source_type'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('knowledgeBases.form.sourceLabel')}</FormLabel>
              <FormControl>
                <RadioGroup
                  value={field.value}
                  onValueChange={field.onChange}
                  className='grid gap-2 sm:grid-cols-3'
                >
                  {(['upload', 'feishu_drive', 'feishu_wiki'] as const).map(
                    (value) => {
                      const label = t(
                        `knowledgeBases.form.sourceOptions.${value}`
                      )
                      return (
                        <FormControl key={value}>
                          <FormLabel className='flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 font-normal has-[button[data-state=checked]]:border-primary'>
                            <RadioGroupItem value={value} aria-label={label} />
                            {label}
                          </FormLabel>
                        </FormControl>
                      )
                    }
                  )}
                </RadioGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        {isFeishu && (
          <>
            <FormField
              control={form.control}
              name='source_connection_id'
              render={({ field, fieldState }) => (
                <FormItem>
                  <FormLabel>
                    {t('knowledgeBases.form.sourceConnectionLabel')}
                  </FormLabel>
                  <FormControl>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <SelectTrigger
                        aria-label={t(
                          'knowledgeBases.form.sourceConnectionLabel'
                        )}
                      >
                        <SelectValue
                          placeholder={t(
                            'knowledgeBases.form.sourceConnectionPlaceholder'
                          )}
                        />
                      </SelectTrigger>
                      <SelectContent>
                        {connections
                          .filter((c) => c.status === 'active')
                          .map((connection) => (
                            <SelectItem
                              key={connection.id}
                              value={connection.id}
                            >
                              {connection.name}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                  </FormControl>
                  {fieldState.error && <FormMessage />}
                </FormItem>
              )}
            />
            {connections.filter((c) => c.status === 'active').length === 0 && (
              <Alert>
                <AlertDescription>
                  {t('knowledgeBases.form.sourceConnectionEmpty')}{' '}
                  <Link
                    to='/workspaces/$workspaceSlug/integrations'
                    params={{ workspaceSlug }}
                    className='text-primary underline-offset-4 hover:underline'
                  >
                    {t('knowledgeBases.form.sourceConnectionGo')}
                  </Link>
                </AlertDescription>
              </Alert>
            )}
            <FormField
              control={form.control}
              name='root_token'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('knowledgeBases.form.sourceTokenLabel')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t(
                        'knowledgeBases.form.sourceTokenPlaceholder'
                      )}
                      autoComplete='off'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('knowledgeBases.form.sourceTokenHint')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='sync_enabled'
              render={({ field }) => (
                <FormItem className='flex min-h-11 items-center justify-between rounded-lg border px-3'>
                  <div className='space-y-0.5'>
                    <FormLabel>
                      {t('knowledgeBases.form.syncScheduleLabel')}
                    </FormLabel>
                    <FormDescription>
                      {t('knowledgeBases.form.syncScheduleHint')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      aria-label={t('knowledgeBases.form.syncScheduleLabel')}
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
            {syncEnabled && (
              <FormField
                control={form.control}
                name='cron'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('knowledgeBases.form.cronLabel')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='0 0 * * *'
                        autoComplete='off'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('knowledgeBases.form.cronHint')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
          </>
        )}
        <FormField
          control={form.control}
          name='strategy'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('knowledgeBases.form.strategyLabel')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {(['auto', 'heading', 'heuristic', 'recursive'] as const).map(
                    (strategy) => (
                      <SelectItem key={strategy} value={strategy}>
                        {t(`knowledgeBases.form.strategyOptions.${strategy}`)}
                      </SelectItem>
                    )
                  )}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='enable_parent_child'
          render={({ field }) => (
            <FormItem className='flex min-h-11 items-center justify-between rounded-lg border px-3'>
              <FormLabel>{t('knowledgeBases.form.parentChildLabel')}</FormLabel>
              <FormControl>
                <Switch
                  aria-label={t('knowledgeBases.form.parentChildLabel')}
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <div className='grid gap-5 sm:grid-cols-2'>
          {form.watch('enable_parent_child') ? (
            <>
              <FormField
                control={form.control}
                name='child_chunk_size'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('knowledgeBases.form.childSizeLabel')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={64}
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
                name='parent_chunk_size'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('knowledgeBases.form.parentSizeLabel')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={512}
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
            </>
          ) : (
            <FormField
              control={form.control}
              name='chunk_size'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('knowledgeBases.form.chunkSizeLabel')}
                  </FormLabel>
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
          )}
          <FormField
            control={form.control}
            name='chunk_overlap'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t(
                    form.watch('enable_parent_child')
                      ? 'knowledgeBases.form.parentOverlapLabel'
                      : 'knowledgeBases.form.chunkOverlapLabel'
                  )}
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
