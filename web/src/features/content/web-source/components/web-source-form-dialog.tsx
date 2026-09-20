import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { WebSourceView } from '../types'
import { WebSourceForm } from './web-source-form'

type WebSourceFormDialogProps = {
  workspaceSlug: string
  kbId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 传入即为编辑态；缺省为创建。 */
  source?: WebSourceView
}

/** 添加/编辑网站源对话框（spec §9.5.3/§9.5.4）。 */
export function WebSourceFormDialog({
  workspaceSlug,
  kbId,
  open,
  onOpenChange,
  source,
}: WebSourceFormDialogProps) {
  const { t } = useTranslation()
  const isEdit = source !== undefined
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90svh] overflow-y-auto sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>
            {isEdit
              ? t('webSource.form.editTitle', { name: source.source.name })
              : t('webSource.form.createTitle')}
          </DialogTitle>
          <DialogDescription>
            {t(
              isEdit
                ? 'webSource.form.editDescription'
                : 'webSource.form.createDescription'
            )}
          </DialogDescription>
        </DialogHeader>
        {open && (
          <WebSourceForm
            workspaceSlug={workspaceSlug}
            kbId={kbId}
            source={source}
            onDone={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
