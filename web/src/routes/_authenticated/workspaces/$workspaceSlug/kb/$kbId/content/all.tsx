import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Plus, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { meQueryOptions } from '@/features/auth/queries'
import { ContentList } from '@/features/content/lists/content-list'
import { documentsQueryOptions } from '@/features/documents/queries'
import { documentStatusSchema } from '@/features/documents/schemas'
import { canManageContent } from '@/features/knowledge-bases/permissions'

const contentSearchSchema = z.object({
  q: z.string().optional(),
  status: documentStatusSchema.optional(),
  sort: z.enum(['updated', 'name']).optional(),
})

function ContentAllPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const { data: documents = [] } = useQuery(
    documentsQueryOptions(workspaceSlug, kbId)
  )
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  const canManage = canManageContent(role)
  const sorted = [...documents].sort((left, right) =>
    search.sort === 'name'
      ? left.title.localeCompare(right.title, 'zh-CN')
      : right.updated_at.localeCompare(left.updated_at)
  )

  return (
    <div className='space-y-4'>
      <div className='flex flex-col justify-between gap-3 lg:flex-row lg:items-center'>
        <div className='flex flex-1 flex-col gap-2 sm:flex-row'>
          <Input
            value={search.q ?? ''}
            placeholder={t(
              'routes.workspaces.kb.content.all.searchPlaceholder'
            )}
            aria-label={t('routes.workspaces.kb.content.all.searchAriaLabel')}
            className='sm:max-w-sm'
            onChange={(event) =>
              void navigate({
                search: { ...search, q: event.target.value || undefined },
                replace: true,
              })
            }
          />
          <Select
            value={search.status ?? 'all'}
            onValueChange={(status) =>
              void navigate({
                search: {
                  ...search,
                  status:
                    status === 'all'
                      ? undefined
                      : (status as z.infer<typeof documentStatusSchema>),
                },
                replace: true,
              })
            }
          >
            <SelectTrigger
              aria-label={t('routes.workspaces.kb.content.all.statusAriaLabel')}
              className='sm:w-40'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>
                {t('routes.workspaces.kb.content.all.statusAll')}
              </SelectItem>
              <SelectItem value='ready'>
                {t('routes.workspaces.kb.content.all.statusReady')}
              </SelectItem>
              <SelectItem value='processing'>
                {t('routes.workspaces.kb.content.all.statusProcessing')}
              </SelectItem>
              <SelectItem value='failed'>
                {t('routes.workspaces.kb.content.all.statusFailed')}
              </SelectItem>
              <SelectItem value='deleting'>
                {t('routes.workspaces.kb.content.all.statusDeleting')}
              </SelectItem>
              <SelectItem value='deleted'>
                {t('routes.workspaces.kb.content.all.statusDeleted')}
              </SelectItem>
            </SelectContent>
          </Select>
          <Select
            value={search.sort ?? 'updated'}
            onValueChange={(sort) =>
              void navigate({
                search: {
                  ...search,
                  sort: sort as 'updated' | 'name',
                },
                replace: true,
              })
            }
          >
            <SelectTrigger
              aria-label={t('routes.workspaces.kb.content.all.sortAriaLabel')}
              className='sm:w-40'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='updated'>
                {t('routes.workspaces.kb.content.all.sortUpdated')}
              </SelectItem>
              <SelectItem value='name'>
                {t('routes.workspaces.kb.content.all.sortName')}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        {canManage && (
          <div className='flex flex-wrap gap-2'>
            <Button asChild variant='outline'>
              <a
                href={`/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/files/upload`}
              >
                <Upload />
                {t('routes.workspaces.kb.content.all.uploadButton')}
              </a>
            </Button>
            <Button asChild>
              <a
                href={`/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/faq/new`}
              >
                <Plus />
                {t('routes.workspaces.kb.content.all.newFaqButton')}
              </a>
            </Button>
          </div>
        )}
      </div>
      <ContentList
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        documents={sorted}
        kind='all'
        query={search.q}
        status={search.status}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/all'
)({
  validateSearch: contentSearchSchema,
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      documentsQueryOptions(params.workspaceSlug, params.kbId)
    ),
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.all.breadcrumb',
    },
  },
  component: ContentAllPage,
})
