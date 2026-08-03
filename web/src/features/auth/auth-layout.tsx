import { useTranslation } from 'react-i18next'
import { Logo } from '@/assets/logo'
import { AppearanceControls } from '@/components/appearance-controls'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  return (
    <div className='container relative grid min-h-svh max-w-none items-center border-primary border-t-[3px]'>
      {/* 登录/注册等未认证页面的主题与语言切换：固定在右上角，不干扰居中的表单 */}
      <AppearanceControls />
      <div className='mx-auto flex w-full flex-col items-center justify-center py-10 sm:p-8'>
        <div
          data-slot='auth-mark'
          className='mb-6 flex items-center justify-center gap-3'
        >
          <Logo className='size-12 shrink-0' aria-hidden='true' />
          <div>
            <h1 className='font-semibold text-xl tracking-tight'>
              {t('auth.layout.brandName')}
            </h1>
            <p className='text-muted-foreground text-xs'>
              {t('auth.layout.tagline')}
            </p>
          </div>
        </div>
        {children}
      </div>
    </div>
  )
}
