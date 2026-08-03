import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type APIKeyRevokeDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  apiKeyName: string
  isLoading: boolean
  error?: string
  onConfirm: () => void
}

export function APIKeyRevokeDialog({
  open,
  onOpenChange,
  apiKeyName,
  isLoading,
  error,
  onConfirm,
}: APIKeyRevokeDialogProps) {
  const { t } = useTranslation()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('apiKeys.revokeDialog.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('apiKeys.revokeDialog.description', { name: apiKeyName })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && (
          <p className='text-destructive text-sm' role='alert'>
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isLoading}>
            {t('apiKeys.revokeDialog.cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            className={cn(buttonVariants({ variant: 'destructive' }))}
            disabled={isLoading}
            onClick={(event) => {
              // 阻止 AlertDialog 默认关闭，交由 mutation 成功后控制。
              event.preventDefault()
              onConfirm()
            }}
          >
            {isLoading && <Loader2 className='animate-spin' />}
            {t('apiKeys.revokeDialog.confirm')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
