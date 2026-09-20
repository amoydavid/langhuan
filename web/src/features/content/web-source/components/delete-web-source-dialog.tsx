import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { parseApiError } from '@/lib/api/error'
import { deleteWebSourceMutationOptions } from '../queries'
import type { WebSourceView } from '../types'

type DeleteWebSourceDialogProps = {
  workspaceSlug: string
  kbId: string
  source: WebSourceView
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 删除成功后回调（如导航回源列表）。 */
  onDeleted: () => void
}

/**
 * 删除内容源确认（spec §9.5.2）：强制选择页面处置——keep（默认）仅移除来源，
 * remove 连同页面软删；后端 409（有在飞同步）经错误提示透出。
 */
export function DeleteWebSourceDialog({
  workspaceSlug,
  kbId,
  source,
  open,
  onOpenChange,
  onDeleted,
}: DeleteWebSourceDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [pages, setPages] = useState<'keep' | 'remove'>('keep')
  const mutation = useMutation(
    deleteWebSourceMutationOptions(workspaceSlug, kbId, queryClient)
  )

  async function handleDelete() {
    try {
      await mutation.mutateAsync({ sourceId: source.source.id, pages })
      toast.success(t('webSource.deleteDialog.deletedToast'))
      onOpenChange(false)
      onDeleted()
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t('webSource.deleteDialog.title', { name: source.source.name })}
          </DialogTitle>
          <DialogDescription>
            {t('webSource.deleteDialog.description')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-2'>
          <Label>{t('webSource.deleteDialog.pagesLabel')}</Label>
          <RadioGroup
            value={pages}
            onValueChange={(value) => setPages(value as 'keep' | 'remove')}
            className='gap-2'
          >
            {(['keep', 'remove'] as const).map((value) => (
              <Label
                key={value}
                htmlFor={`delete-pages-${value}`}
                className='flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2 font-normal has-[button[data-state=checked]]:border-primary'
              >
                <RadioGroupItem
                  value={value}
                  id={`delete-pages-${value}`}
                  className='mt-0.5'
                />
                <span className='space-y-0.5'>
                  <span className='block font-medium text-sm'>
                    {t(
                      value === 'keep'
                        ? 'webSource.deleteDialog.pagesKeepLabel'
                        : 'webSource.deleteDialog.pagesRemoveLabel'
                    )}
                  </span>
                  <span className='block text-muted-foreground text-xs'>
                    {t(
                      value === 'keep'
                        ? 'webSource.deleteDialog.pagesKeepDescription'
                        : 'webSource.deleteDialog.pagesRemoveDescription',
                      { count: source.page_count }
                    )}
                  </span>
                </span>
              </Label>
            ))}
          </RadioGroup>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t('webSource.deleteDialog.cancel')}
          </Button>
          <Button
            type='button'
            variant='destructive'
            disabled={mutation.isPending}
            onClick={() => void handleDelete()}
          >
            {mutation.isPending ? <Loader2 className='animate-spin' /> : null}
            {t('webSource.deleteDialog.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
