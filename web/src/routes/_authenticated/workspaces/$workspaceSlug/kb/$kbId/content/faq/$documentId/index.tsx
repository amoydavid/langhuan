import { useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { FAQDetail } from '@/features/content/faq-editor/faq-detail'
import { faqDocumentQueryOptions } from '@/features/content/faq-editor/queries'
import { canonicalDocumentHref } from '@/features/content/routing'
import i18n from '@/lib/i18n'

type FAQDetailLoaderData = { documentName: string }

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

function FAQDetailPage() {
  const { workspaceSlug, kbId, documentId } = Route.useParams()
  const { data: faq } = useQuery(
    faqDocumentQueryOptions(workspaceSlug, documentId)
  )
  if (!faq) return null
  return (
    <FAQDetail
      faq={faq}
      canEdit
      editHref={`/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/faq/${encodeURIComponent(documentId)}/edit`}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/faq/$documentId/'
)({
  loader: async ({ context, params }): Promise<FAQDetailLoaderData> => {
    const faq = await context.queryClient.ensureQueryData(
      faqDocumentQueryOptions(params.workspaceSlug, params.documentId)
    )
    if (
      faq.document.kind !== 'faq' ||
      faq.document.knowledge_base_id !== params.kbId
    ) {
      throw redirect({
        href: canonicalDocumentHref(params.workspaceSlug, faq.document),
      })
    }
    return {
      documentName:
        faq.document.title ||
        i18n.t('routes.workspaces.kb.content.faq.detail.unnamedTitle'),
    }
  },
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.faq.detail.breadcrumb',
      resolve: loaderDocumentName,
    },
  },
  component: FAQDetailPage,
})
