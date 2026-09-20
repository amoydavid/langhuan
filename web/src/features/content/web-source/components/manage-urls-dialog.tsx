import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { parseApiError } from '@/lib/api/error'
import { readWebSourceConfig } from '../config'
import { parseURLLines, partitionDuplicateURLs } from '../form-schema'
import { updateWebSourceMutationOptions } from '../queries'
import type { WebSourceView } from '../types'

type ManageUrlsDialogProps = {
  workspaceSlug: string
  kbId: string
  source: WebSourceView
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** urls 模式的页面清单管理（spec §9.5.5）：勾选移除 + 多行新增，保存整体替换。 */
export function ManageUrlsDialog({
  workspaceSlug,
  kbId,
  source,
  open,
  onOpenChange,
}: ManageUrlsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const config = readWebSourceConfig(source.source.config)
  const currentURLs = config.urls
  const [removed, setRemoved] = useState<Set<string>>(new Set())
  const [addedText, setAddedText] = useState('')
  const [query, setQuery] = useState('')

  const mutation = useMutation(
    updateWebSourceMutationOptions(
      workspaceSlug,
      kbId,
      source.source.id,
      queryClient
    )
  )

  const visibleURLs = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase()
    return currentURLs.filter(
      (url) =>
        normalized.length === 0 || url.toLocaleLowerCase().includes(normalized)
    )
  }, [currentURLs, query])

  const added = parseURLLines(addedText)
  // 保留清单 = 现有 − 勾选移除；新增行与之去重（重复行预览提示并忽略，
  // 避免整体替换后出现重复项被后端 400 拒绝；被移除的 URL 允许加回）。
  const keptURLs = currentURLs.filter((url) => !removed.has(url))
  const { fresh, duplicates } = partitionDuplicateURLs(added.valid, keptURLs)
  const removedCount = currentURLs.length - keptURLs.length
  const nextURLCount = keptURLs.length + fresh.length

  function toggle(url: string, checked: boolean) {
    setRemoved((previous) => {
      const next = new Set(previous)
      if (checked) {
        next.delete(url)
      } else {
        next.add(url)
      }
      return next
    })
  }

  const allVisibleKept = visibleURLs.every((url) => !removed.has(url))

  function toggleAllVisible() {
    setRemoved((previous) => {
      const next = new Set(previous)
      if (allVisibleKept) {
        for (const url of visibleURLs) next.add(url)
      } else {
        for (const url of visibleURLs) next.delete(url)
      }
      return next
    })
  }

  async function handleSave() {
    const nextURLs = [...keptURLs, ...fresh]
    try {
      const result = await mutation.mutateAsync({ config: { urls: nextURLs } })
      toast.success(t('webSource.manageUrls.savedToast'), {
        description: result.job_id,
      })
      onOpenChange(false)
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90svh] overflow-y-auto sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>
            {t('webSource.manageUrls.title', { name: source.source.name })}
          </DialogTitle>
          <DialogDescription>
            {t('webSource.manageUrls.description', {
              count: currentURLs.length,
            })}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-3'>
          <div className='flex items-center gap-2'>
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t('webSource.manageUrls.searchPlaceholder')}
              aria-label={t('webSource.manageUrls.searchAriaLabel')}
              className='sm:max-w-xs'
            />
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={visibleURLs.length === 0}
              onClick={toggleAllVisible}
            >
              {t('webSource.manageUrls.selectAll')}
            </Button>
          </div>

          <div className='space-y-1'>
            <Label>{t('webSource.manageUrls.currentLabel')}</Label>
            <div className='max-h-56 overflow-y-auto rounded-lg border px-1 py-1'>
              {visibleURLs.length === 0 ? (
                <p className='px-2 py-3 text-muted-foreground text-sm'>
                  {t('webSource.manageUrls.noMatches')}
                </p>
              ) : (
                visibleURLs.map((url) => {
                  const kept = !removed.has(url)
                  return (
                    <Label
                      key={url}
                      htmlFor={`manage-urls-${url}`}
                      className='flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 font-normal hover:bg-muted/50'
                    >
                      <Checkbox
                        id={`manage-urls-${url}`}
                        checked={kept}
                        onCheckedChange={(checked) =>
                          toggle(url, checked === true)
                        }
                        aria-label={url}
                      />
                      <span
                        className={
                          kept
                            ? 'truncate font-mono text-xs'
                            : 'font-mono text-muted-foreground text-xs line-through'
                        }
                      >
                        {url}
                      </span>
                    </Label>
                  )
                })
              )}
            </div>
          </div>

          <div className='space-y-1'>
            <Label htmlFor='web-source-manage-urls-added'>
              {t('webSource.manageUrls.addLabel')}
            </Label>
            <Textarea
              id='web-source-manage-urls-added'
              className='min-h-24 font-mono text-xs'
              placeholder={t('webSource.manageUrls.addPlaceholder')}
              value={addedText}
              onChange={(event) => setAddedText(event.target.value)}
            />
          </div>

          <div className='space-y-1 rounded-lg bg-muted/40 px-3 py-2 text-sm'>
            <p>
              {t('webSource.manageUrls.previewAdded', { count: fresh.length })}
              {' · '}
              {t('webSource.manageUrls.previewRemoved', {
                count: removedCount,
              })}
            </p>
            {duplicates.length > 0 && (
              <p className='text-warning text-xs' role='alert'>
                {t('webSource.manageUrls.previewDuplicate', {
                  count: duplicates.length,
                  samples: duplicates.slice(0, 3).join('、'),
                })}
              </p>
            )}
            {added.invalid.length > 0 && (
              <p className='text-warning text-xs' role='alert'>
                {t('webSource.manageUrls.previewInvalid', {
                  count: added.invalid.length,
                  samples: added.invalid.slice(0, 3).join('、'),
                })}
              </p>
            )}
            <p className='text-muted-foreground text-xs'>
              {t(
                config.onDelete === 'remove'
                  ? 'webSource.manageUrls.removePolicyRemove'
                  : 'webSource.manageUrls.removePolicyKeep'
              )}
            </p>
          </div>
        </div>

        {mutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(mutation.error).message}
          </p>
        )}

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t('webSource.manageUrls.cancel')}
          </Button>
          <Button
            type='button'
            disabled={
              mutation.isPending ||
              nextURLCount === 0 ||
              (fresh.length === 0 && removedCount === 0)
            }
            onClick={() => void handleSave()}
          >
            {mutation.isPending ? <Loader2 className='animate-spin' /> : null}
            {t('webSource.manageUrls.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
