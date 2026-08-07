import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Building2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { meQueryOptions } from '@/features/auth/queries'
import type { Role } from '@/features/auth/types'

export function WorkspacePicker() {
  const { t } = useTranslation()
  const roleLabel: Record<Role, string> = {
    owner: t('workspaces.picker.roles.owner'),
    admin: t('workspaces.picker.roles.admin'),
    member: t('workspaces.picker.roles.member'),
  }
  const { data: me } = useQuery(meQueryOptions())
  if (!me) return null

  return (
    <div className='space-y-6'>
      <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
        <div>
          <p className='page-eyebrow'>{t('workspaces.picker.eyebrow')}</p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('workspaces.picker.title')}
          </h1>
          <p className='mt-2 max-w-2xl text-muted-foreground'>
            {t('workspaces.picker.description')}
          </p>
        </div>
        {me.user.is_platform_admin &&
          !(me.single_tenant && me.workspaces.length > 0) && (
            <Button asChild>
              <Link to='/workspaces/new'>
                <Plus />
                {t('workspaces.picker.createButton')}
              </Link>
            </Button>
          )}
      </div>

      {me.workspaces.length === 0 ? (
        <Card className='border-dashed'>
          <CardContent className='flex min-h-48 flex-col items-center justify-center text-center'>
            <div className='mb-4 flex size-11 items-center justify-center rounded-lg bg-muted'>
              <Building2 className='size-5 text-muted-foreground' />
            </div>
            <h2 className='font-medium'>{t('workspaces.picker.emptyTitle')}</h2>
            <p className='mt-2 max-w-md text-muted-foreground text-sm'>
              {me.user.is_platform_admin
                ? t('workspaces.picker.emptyAdminDescription')
                : t('workspaces.picker.emptyMemberDescription')}
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
          {me.workspaces.map((workspace) => (
            <Card key={workspace.workspace_id} className='group resource-card'>
              <CardHeader>
                <div className='icon-tile mb-3'>
                  <Building2 className='size-5' />
                </div>
                <CardTitle>{workspace.name}</CardTitle>
                <CardDescription>
                  {workspace.slug} · {roleLabel[workspace.role]}
                </CardDescription>
                <CardAction>
                  <Button variant='ghost' size='icon' asChild>
                    <Link
                      to='/workspaces/$workspaceSlug/kb'
                      params={{ workspaceSlug: workspace.slug }}
                      aria-label={t('workspaces.picker.enterWorkspace', {
                        name: workspace.name,
                      })}
                    >
                      <ArrowRight className='transition-transform group-hover:translate-x-0.5' />
                    </Link>
                  </Button>
                </CardAction>
              </CardHeader>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
