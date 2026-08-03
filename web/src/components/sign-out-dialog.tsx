import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { logout } from '@/features/auth/api'
import { safeRedirect } from '@/features/auth/navigation'
import { parseApiError } from '@/lib/api/error'

interface SignOutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SignOutDialog({ open, onOpenChange }: SignOutDialogProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => logout(),
    onSuccess: async () => {
      const redirect = safeRedirect(location.href)
      queryClient.clear()
      onOpenChange(false)
      await navigate({
        to: '/sign-in',
        search: { redirect },
        replace: true,
      })
    },
    onError: (error) => toast.error(parseApiError(error).message),
  })

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('common.signOut')}
      desc={t('common.signOutDescription')}
      cancelBtnText={t('common.cancel')}
      confirmText={t('common.signOut')}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
      className='sm:max-w-sm'
    />
  )
}
