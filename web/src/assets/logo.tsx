import type { SVGProps } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

type LogoVariant = 'fill' | 'line'

type LogoProps = SVGProps<SVGSVGElement> & {
  variant?: LogoVariant
}

const bubblePath =
  'M32 22h36a10 10 0 0 1 10 10v20a10 10 0 0 1-10 10H50L36 78V62h-4a10 10 0 0 1-10-10V32a10 10 0 0 1 10-10z'

export function Logo({ className, variant = 'fill', ...props }: LogoProps) {
  const { t } = useTranslation()
  return (
    <svg
      id='langhuan-logo'
      data-logo-variant={variant}
      viewBox='0 0 100 100'
      xmlns='http://www.w3.org/2000/svg'
      role='img'
      className={cn('size-6', className)}
      {...props}
    >
      <title>{t('common.brandName')}</title>
      {variant === 'fill' ? (
        <>
          <path fill='#00863b' d={bubblePath} />
          <path fill='#ffffff' d='M54 22h14v22l-7-6-7 6V22z' />
        </>
      ) : (
        <>
          <path
            fill='none'
            stroke='currentColor'
            strokeWidth='6'
            strokeLinejoin='round'
            d={bubblePath}
          />
          <path
            fill='none'
            stroke='currentColor'
            strokeWidth='6'
            strokeLinejoin='round'
            d='M54 22v22l7-6 7 6v-22'
          />
        </>
      )}
    </svg>
  )
}
