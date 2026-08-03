import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout/main'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { meQueryOptions } from '@/features/auth/queries'
import { KnowledgeBaseForm } from '@/features/knowledge-bases/components/knowledge-base-form'
import { selectableModelsQueryOptions } from '@/features/models/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/new'
)({
  loader: async ({ context, params }) => {
    const [me] = await Promise.all([
      context.queryClient.ensureQueryData(meQueryOptions()),
      context.queryClient.ensureQueryData(
        selectableModelsQueryOptions(params.workspaceSlug)
      ),
    ])
    return me
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.new.breadcrumb' },
  },
  component: NewKnowledgeBasePage,
})

function NewKnowledgeBasePage() {
  const { t } = useTranslation()
  const { workspaceSlug } = Route.useParams()
  const me = Route.useLoaderData()
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  if (!membership) return null
  return (
    <Main>
      <div className='mx-auto max-w-3xl space-y-6'>
        <div>
          <p className='page-eyebrow'>
            {t('routes.workspaces.kb.new.eyebrow')}
          </p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('routes.workspaces.kb.new.title')}
          </h1>
          <p className='mt-2 text-muted-foreground'>
            {t('routes.workspaces.kb.new.description')}
          </p>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{t('routes.workspaces.kb.new.cardTitle')}</CardTitle>
            <CardDescription>
              {t('routes.workspaces.kb.new.cardDescription')}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <KnowledgeBaseForm
              workspaceSlug={workspaceSlug}
              workspaceRole={membership.role}
            />
          </CardContent>
        </Card>
      </div>
    </Main>
  )
}
