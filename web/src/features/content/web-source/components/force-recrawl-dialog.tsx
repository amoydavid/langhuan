import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type ForceRecrawlDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 该源当前页面数（文案中说明全量重抓规模）。 */
  pageCount: number
  /** 确认回调：调用方以 force=true 触发既有 sync mutation；抛错时对话框保持打开。 */
  onConfirm: () => Promise<void>
}

/**
 * 强制重爬确认（v1.4.0 spec §9.3.7）：说明忽略变更检测、全量重抓的成本；
 * 列表卡片与源详情共用。确认后走既有 `POST .../web-sources/{id}/sync {"force":true}`。
 */
export function ForceRecrawlDialog({
  open,
  onOpenChange,
  pageCount,
  onConfirm,
}: ForceRecrawlDialogProps) {
  const { t } = useTranslation()
  const [pending, setPending] = useState(false)

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (pending) return
        onOpenChange(next)
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('webSource.forceRecrawl.title')}</DialogTitle>
          <DialogDescription>
            {t('webSource.forceRecrawl.description', { pages: pageCount })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            {t('webSource.forceRecrawl.cancel')}
          </Button>
          <Button
            type='button'
            variant='destructive'
            size='sm'
            disabled={pending}
            onClick={async () => {
              setPending(true)
              try {
                await onConfirm()
                onOpenChange(false)
              } finally {
                setPending(false)
              }
            }}
          >
            {pending ? <Loader2 className='animate-spin' /> : null}
            {t('webSource.forceRecrawl.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
