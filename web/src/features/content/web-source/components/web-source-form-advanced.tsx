import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
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
import { sourceConnectionsQueryOptions } from '@/features/integrations/queries'
import { cn } from '@/lib/utils'
import {
  engineConnectionProviders,
  engineRequiresConnection,
  type WebSourceFormValues,
} from '../form-schema'
import {
  NumberStringField,
  ReadOnlyField,
  SwitchField,
} from './web-source-form-fields'

const onDeleteLabelKeys = {
  keep: 'webSource.form.onDeleteKeep',
  remove: 'webSource.form.onDeleteRemove',
} as const

type WebSourceAdvancedOptionsProps = {
  form: UseFormReturn<WebSourceFormValues>
  workspaceSlug: string
  /** 编辑态：引擎与连接为不可变项，只读展示。 */
  isEdit: boolean
}

/** 高级选项折叠区（spec §9.5.4）：抓取上限/路径过滤/引擎与连接/正文抽取/周期/删除策略。 */
export function WebSourceAdvancedOptions({
  form,
  workspaceSlug,
  isEdit,
}: WebSourceAdvancedOptionsProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const mode = form.watch('mode')
  const engine = form.watch('engine')
  const extractMode = form.watch('extract_mode')
  const syncEnabled = form.watch('sync_enabled')

  const { data: connections = [] } = useQuery(
    sourceConnectionsQueryOptions(workspaceSlug)
  )
  const connectionProviders = engineConnectionProviders(engine)
  const usableConnections = connections.filter(
    (connection) =>
      connection.status === 'active' &&
      connectionProviders.includes(connection.provider)
  )
  const selectedConnection = connections.find(
    (connection) => connection.id === form.watch('connection_id')
  )

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='rounded-lg border'
    >
      <CollapsibleTrigger className='flex w-full items-center justify-between px-3 py-2 font-medium text-sm'>
        <span>
          {t('webSource.form.advancedTitle')}
          <span className='ml-2 font-normal text-muted-foreground text-xs'>
            {t('webSource.form.advancedSummary')}
          </span>
        </span>
        <ChevronDown
          className={cn('size-4 transition-transform', open && 'rotate-180')}
        />
      </CollapsibleTrigger>
      <CollapsibleContent className='space-y-5 border-t px-3 py-4'>
        {mode === 'crawl' && (
          <>
            <div className='grid gap-5 sm:grid-cols-2'>
              <NumberStringField
                control={form.control}
                name='max_pages'
                label={t('webSource.form.maxPagesLabel')}
              />
              <NumberStringField
                control={form.control}
                name='max_depth'
                label={t('webSource.form.maxDepthLabel')}
              />
            </div>
            <FormField
              control={form.control}
              name='include_paths'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('webSource.form.includePathsLabel')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('webSource.form.includePathsPlaceholder')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='exclude_paths'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('webSource.form.excludePathsLabel')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('webSource.form.excludePathsPlaceholder')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('webSource.form.pathsHint')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <SwitchField
              control={form.control}
              name='allow_subdomains'
              label={t('webSource.form.allowSubdomainsLabel')}
            />
          </>
        )}

        <SwitchField
          control={form.control}
          name='respect_robots'
          label={t('webSource.form.respectRobotsLabel')}
        />
        <FormField
          control={form.control}
          name='request_delay_ms'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('webSource.form.requestDelayLabel')}</FormLabel>
              <FormControl>
                <Input
                  inputMode='numeric'
                  placeholder={t('webSource.form.requestDelayPlaceholder')}
                  className='sm:max-w-40'
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {isEdit ? (
          <div className='grid gap-5 sm:grid-cols-2'>
            <ReadOnlyField
              label={t('webSource.form.engineLabel')}
              value={t(`webSource.engine.${engine}`)}
            />
            <ReadOnlyField
              label={t('webSource.form.connectionLabel')}
              value={
                selectedConnection?.name ??
                form.getValues('connection_id') ??
                '—'
              }
            />
          </div>
        ) : (
          <>
            <FormField
              control={form.control}
              name='engine'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('webSource.form.engineLabel')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger
                        aria-label={t('webSource.form.engineLabel')}
                      >
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {(
                        ['http', 'auto', 'render', 'firecrawl', 'jina'] as const
                      ).map((value) => (
                        <SelectItem key={value} value={value}>
                          {t(`webSource.engine.${value}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t('webSource.form.engineHint')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            {engineRequiresConnection(engine) && (
              <>
                <FormField
                  control={form.control}
                  name='connection_id'
                  render={({ field, fieldState }) => (
                    <FormItem>
                      <FormLabel>
                        {t('webSource.form.connectionLabel')}
                      </FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger
                            aria-label={t('webSource.form.connectionLabel')}
                          >
                            <SelectValue
                              placeholder={t('webSource.form.connectionLabel')}
                            />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {usableConnections.map((connection) => (
                            <SelectItem
                              key={connection.id}
                              value={connection.id}
                            >
                              {connection.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {t('webSource.form.connectionHint')}
                      </FormDescription>
                      {fieldState.error && <FormMessage />}
                    </FormItem>
                  )}
                />
                {usableConnections.length === 0 && (
                  <Alert>
                    <AlertDescription>
                      {t('webSource.form.connectionEmpty')}{' '}
                      <Link
                        to='/workspaces/$workspaceSlug/integrations'
                        params={{ workspaceSlug }}
                        className='text-primary underline-offset-4 hover:underline'
                      >
                        {t('webSource.form.connectionGo')}
                      </Link>
                    </AlertDescription>
                  </Alert>
                )}
              </>
            )}
          </>
        )}

        <div className='grid gap-5 sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='extract_mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('webSource.form.extractModeLabel')}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <FormControl>
                    <SelectTrigger
                      aria-label={t('webSource.form.extractModeLabel')}
                    >
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value='readability'>
                      {t('webSource.form.extractReadability')}
                    </SelectItem>
                    <SelectItem value='full'>
                      {t('webSource.form.extractFull')}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          {extractMode === 'readability' && (
            <SwitchField
              control={form.control}
              name='fallback_to_full'
              label={t('webSource.form.fallbackToFullLabel')}
            />
          )}
        </div>

        <SwitchField
          control={form.control}
          name='sync_enabled'
          label={t('webSource.form.syncScheduleLabel')}
          description={t('webSource.form.syncScheduleHint')}
        />
        {syncEnabled && (
          <FormField
            control={form.control}
            name='cron'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('webSource.form.cronLabel')}</FormLabel>
                <FormControl>
                  <Input placeholder='0 */6 * * *' {...field} />
                </FormControl>
                <FormDescription>
                  {t('webSource.form.cronHint')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        <FormField
          control={form.control}
          name='on_delete'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('webSource.form.onDeleteLabel')}</FormLabel>
              <FormControl>
                <RadioGroup
                  value={field.value}
                  onValueChange={field.onChange}
                  className='flex flex-wrap gap-2'
                >
                  {(['keep', 'remove'] as const).map((value) => (
                    // 原生 label htmlFor → item id：整卡可点（理由同导入模式卡片）。
                    <label
                      key={value}
                      htmlFor={`web-source-on-delete-${value}`}
                      className='flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 font-normal has-[button[data-state=checked]]:border-primary'
                    >
                      <RadioGroupItem
                        value={value}
                        id={`web-source-on-delete-${value}`}
                      />
                      {t(onDeleteLabelKeys[value])}
                    </label>
                  ))}
                </RadioGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </CollapsibleContent>
    </Collapsible>
  )
}
