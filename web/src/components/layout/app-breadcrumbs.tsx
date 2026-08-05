import { Link, useMatches } from '@tanstack/react-router'
import { ChevronRight } from 'lucide-react'
import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'

export type RouteBreadcrumb = {
  label: string
  resolve?: (loaderData: unknown) => string | undefined
}

declare module '@tanstack/react-router' {
  interface StaticDataRouteOption {
    breadcrumb?: RouteBreadcrumb
    /** 路由需要占满视口剩余高度并启用栏内独立滚动（如文件浏览器）。 */
    fullHeight?: boolean
  }
}

export function AppBreadcrumbs() {
  const { t } = useTranslation()
  const matches = useMatches()
  const crumbs = matches.flatMap((match) => {
    const breadcrumb = match.staticData.breadcrumb
    if (!breadcrumb) return []
    const label = breadcrumb.resolve?.(match.loaderData) ?? breadcrumb.label
    if (!label) return []
    return [
      { id: match.id, label, pathname: match.pathname, status: match.status },
    ]
  })

  if (crumbs.length === 0) {
    return (
      <span className='text-muted-foreground text-sm'>
        {t('common.brandName')}
      </span>
    )
  }

  return (
    <nav aria-label={t('common.breadcrumbsAriaLabel')} className='min-w-0'>
      <ol className='flex min-w-0 items-center gap-1 text-sm'>
        {crumbs.map((crumb, index) => {
          const current = index === crumbs.length - 1
          // breadcrumb label 约定：静态 label 存 i18n key（routes.*），
          // resolve 返回的动态数据（如文档标题）不是 key，t() 会原样返回。
          const label = t(crumb.label as never) as string
          return (
            <Fragment key={crumb.id}>
              {index > 0 && (
                <ChevronRight
                  aria-hidden='true'
                  className='size-4 shrink-0 text-muted-foreground'
                />
              )}
              <li className='min-w-0'>
                {crumb.status === 'pending' ? (
                  <Skeleton className='h-4 w-20' />
                ) : current ? (
                  <span
                    className='block truncate font-medium'
                    aria-current='page'
                  >
                    {label}
                  </span>
                ) : (
                  <Link
                    to={crumb.pathname}
                    className='block truncate text-muted-foreground transition-colors hover:text-foreground'
                  >
                    {label}
                  </Link>
                )}
              </li>
            </Fragment>
          )
        })}
      </ol>
    </nav>
  )
}
