import { useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { DocumentPreview } from '@/features/content/document-preview/document-preview'
import { canonicalDocumentHref } from '@/features/content/routing'
import { documentQueryOptions } from '@/features/documents/queries'
import i18n from '@/lib/i18n'

type WebDetailLoaderData = { documentName: string }

function loaderDocumentName(data: unknown) {
  if (
    typeof data === 'object' &&
    data !== null &&
    'documentName' in data &&
    typeof data.documentName === 'string'
  ) {
    return data.documentName
  }
  return undefined
}

function WebDetailPage() {
  const { t } = useTranslation()
  const { workspaceSlug, documentId } = Route.useParams()
  const { data: item } = useQuery(
    documentQueryOptions(workspaceSlug, documentId)
  )
  if (!item) return null
  return (
    <div className='mx-auto max-w-5xl'>
      <DocumentPreview
        document={item}
        displayName={
          item.title ||
          t('routes.workspaces.kb.content.web.detail.unnamedTitle')
        }
        path={item.source_uri || undefined}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/web/$documentId'
)({
  loader: async ({ context, params }): Promise<WebDetailLoaderData> => {
    const item = await context.queryClient.ensureQueryData(
      documentQueryOptions(params.workspaceSlug, params.documentId)
    )
    if (item.kind !== 'web' || item.knowledge_base_id !== params.kbId) {
      throw redirect({
        href: canonicalDocumentHref(params.workspaceSlug, item),
      })
    }
    return {
      documentName:
        item.title ||
        i18n.t('routes.workspaces.kb.content.web.detail.unnamedTitle'),
    }
  },
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.web.detail.breadcrumb',
      resolve: loaderDocumentName,
    },
  },
  component: WebDetailPage,
})
