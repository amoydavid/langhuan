import { KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { startOIDCLogin } from '@/features/auth/api'

interface OIDCLoginButtonProps {
  /** 登录后跳转路径。 */
  next?: string
  /** 邀请 token（邀请接受流程）。 */
  invitationToken?: string
}

/**
 * OIDC/SSO 登录按钮。点击跳转到后端 /auth/oidc/login，后端 302 到 IdP。
 */
export function OIDCLoginButton({
  next,
  invitationToken,
}: OIDCLoginButtonProps) {
  const { t } = useTranslation()
  return (
    <Button
      type='button'
      variant='outline'
      className='w-full'
      onClick={() =>
        startOIDCLogin({
          next: next ?? '/',
          invitationToken,
        })
      }
    >
      <KeyRound />
      {t('auth.signIn.ssoButton')}
    </Button>
  )
}
