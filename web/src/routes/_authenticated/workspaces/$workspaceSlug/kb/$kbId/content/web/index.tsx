import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ContentList } from '@/features/content/lists/content-list'
import { documentsQueryOptions } from '@/features/documents/queries'

const webSearchSchema = z.object({
  q: z.string().optional(),
  status: z.enum(['ready', 'processing', 'failed']).optional(),
})

function WebContentPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const { data: documents = [] } = useQuery(
    documentsQueryOptions(workspaceSlug, kbId)
  )
  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-2 sm:flex-row'>
        <Input
          value={search.q ?? ''}
          aria-label={t('routes.workspaces.kb.content.web.searchAriaLabel')}
          placeholder={t('routes.workspaces.kb.content.web.searchPlaceholder')}
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
                    : (status as 'ready' | 'processing' | 'failed'),
              },
              replace: true,
            })
          }
        >
          <SelectTrigger
            aria-label={t('routes.workspaces.kb.content.web.statusAriaLabel')}
            className='sm:w-40'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>
              {t('routes.workspaces.kb.content.web.statusAll')}
            </SelectItem>
            <SelectItem value='ready'>
              {t('routes.workspaces.kb.content.web.statusReady')}
            </SelectItem>
            <SelectItem value='processing'>
              {t('routes.workspaces.kb.content.web.statusProcessing')}
            </SelectItem>
            <SelectItem value='failed'>
              {t('routes.workspaces.kb.content.web.statusFailed')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <ContentList
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        documents={documents}
        kind='web'
        query={search.q}
        status={search.status}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/web/'
)({
  validateSearch: webSearchSchema,
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      documentsQueryOptions(params.workspaceSlug, params.kbId)
    ),
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.web.breadcrumb',
    },
  },
  component: WebContentPage,
})
