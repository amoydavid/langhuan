import { Info, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type QueryLogAlertProps = {
  /** 确认开启：resolve true 表示成功（关闭弹窗），false 表示失败（保持弹窗）。 */
  onEnable: () => Promise<boolean>
  enablePending?: boolean
}

/**
 * 未开启查询原文记录时的提示条 + 开启确认（spec §9.3.2）：
 * 说明只有次数与趋势可看；开启需二次确认（保留 30 天、仅管理员可读、可关闭）。
 */
export function QueryLogAlert({ onEnable, enablePending }: QueryLogAlertProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [confirming, setConfirming] = useState(false)

  async function handleConfirm() {
    setConfirming(true)
    try {
      const ok = await onEnable()
      if (ok) setOpen(false)
    } finally {
      setConfirming(false)
    }
  }

  const pending = enablePending ?? confirming

  return (
    <>
      <Alert className='mb-4'>
        <Info />
        <AlertTitle>{t('insights.zeroRecall.disabledTitle')}</AlertTitle>
        <AlertDescription className='sm:flex sm:items-center sm:justify-between sm:gap-3'>
          <span className='leading-6'>
            {t('insights.zeroRecall.disabledDescription')}
          </span>
          <Button
            type='button'
            size='sm'
            variant='outline'
            className='shrink-0'
            onClick={() => setOpen(true)}
          >
            {t('insights.zeroRecall.enableButton')}
          </Button>
        </AlertDescription>
      </Alert>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>
              {t('insights.zeroRecall.enableDialog.title')}
            </DialogTitle>
            <DialogDescription>
              {t('insights.zeroRecall.enableDialog.description')}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              disabled={pending}
              onClick={() => setOpen(false)}
            >
              {t('insights.zeroRecall.enableDialog.cancel')}
            </Button>
            <Button
              type='button'
              disabled={pending}
              onClick={() => void handleConfirm()}
            >
              {pending ? <Loader2 className='animate-spin' /> : null}
              {t('insights.zeroRecall.enableDialog.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
