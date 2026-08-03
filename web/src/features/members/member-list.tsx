import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { Role } from '@/features/auth/types'
import { formatDateTime } from '@/lib/i18n/datetime'
import { MemberActions } from './components/member-actions'
import { membersQueryOptions } from './queries'

type MemberListProps = {
  workspaceSlug: string
  actorRole: Role
  isPlatformAdmin: boolean
}

export function MemberList({
  workspaceSlug,
  actorRole,
  isPlatformAdmin,
}: MemberListProps) {
  const { t } = useTranslation()
  const { data: members = [] } = useQuery(membersQueryOptions(workspaceSlug))
  const roleLabel: Record<Role, string> = {
    owner: t('members.list.role.owner'),
    admin: t('members.list.role.admin'),
    member: t('members.list.role.member'),
  }
  const formatDate = (value: string) =>
    formatDateTime(value, { dateStyle: 'medium' })

  return (
    <>
      <div className='hidden overflow-hidden rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('members.list.columnUser')}</TableHead>
              <TableHead>{t('members.list.columnRole')}</TableHead>
              <TableHead>{t('members.list.columnJoinedAt')}</TableHead>
              <TableHead className='text-right'>
                {t('members.list.columnActions')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => (
              <TableRow key={member.id}>
                <TableCell>
                  <div className='font-medium'>
                    {member.user?.nickname || t('members.list.unnamedUser')}
                  </div>
                  <div className='mt-1 text-muted-foreground text-xs'>
                    {member.user?.email || member.user_id}
                  </div>
                </TableCell>
                <TableCell>
                  <Badge variant='outline'>{roleLabel[member.role]}</Badge>
                </TableCell>
                <TableCell>{formatDate(member.created_at)}</TableCell>
                <TableCell>
                  <MemberActions
                    workspaceSlug={workspaceSlug}
                    actorRole={actorRole}
                    isPlatformAdmin={isPlatformAdmin}
                    member={member}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className='grid gap-3 md:hidden'>
        {members.map((member) => (
          <Card key={member.id}>
            <CardContent className='space-y-4'>
              <div>
                <div className='font-medium'>
                  {member.user?.nickname || t('members.list.unnamedUser')}
                </div>
                <div className='mt-1 break-all text-muted-foreground text-sm'>
                  {member.user?.email || member.user_id}
                </div>
              </div>
              <div className='flex items-center justify-between text-sm'>
                <Badge variant='outline'>{roleLabel[member.role]}</Badge>
                <span className='text-muted-foreground'>
                  {formatDate(member.created_at)}
                </span>
              </div>
              <MemberActions
                workspaceSlug={workspaceSlug}
                actorRole={actorRole}
                isPlatformAdmin={isPlatformAdmin}
                member={member}
              />
            </CardContent>
          </Card>
        ))}
      </div>

      {members.length === 0 && (
        <div className='rounded-lg border border-dashed p-10 text-center text-muted-foreground text-sm'>
          {t('members.list.empty')}
        </div>
      )}
    </>
  )
}
