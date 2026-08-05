import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { DocumentUploadForm } from '@/features/documents/components/document-upload-form'

type UploadFileDialogProps = {
  workspaceSlug: string
  kbId: string
  parentNodeId?: string
  parentPath: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UploadFileDialog({
  workspaceSlug,
  kbId,
  parentNodeId,
  parentPath,
  open,
  onOpenChange,
}: UploadFileDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90svh] overflow-y-auto sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>{t('content.fileUpload.modalTitle')}</DialogTitle>
          <DialogDescription>
            {t('content.fileUpload.description', { path: parentPath || '/' })}
          </DialogDescription>
        </DialogHeader>
        <DocumentUploadForm
          workspaceSlug={workspaceSlug}
          kbId={kbId}
          parentNodeId={parentNodeId}
          onUploaded={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}
