import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  getFAQDocument,
  updateFAQDocument,
} from '@/features/content/faq-editor/api'
import { FAQForm } from '@/features/content/faq-editor/faq-form'
import {
  faqDocumentQueryKey,
  faqDocumentQueryOptions,
  invalidateFAQExperience,
} from '@/features/content/faq-editor/queries'
import type { FAQSaveInput } from '@/features/content/faq-editor/schemas'
import { canonicalDocumentHref } from '@/features/content/routing'
import i18n from '@/lib/i18n'

type FAQEditLoaderData = { documentName: string }

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

function FAQEditPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId, documentId } = Route.useParams()
  const queryClient = useQueryClient()
  const { data: faq } = useQuery(
    faqDocumentQueryOptions(workspaceSlug, documentId)
  )
  if (!faq) return null
  const detailHref = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/faq/${encodeURIComponent(documentId)}`

  async function saveFAQ(input: FAQSaveInput) {
    if (!('base_revision_id' in input)) {
      throw new Error('更新 FAQ 必须包含 base_revision_id')
    }
    return updateFAQDocument(workspaceSlug, documentId, input)
  }

  return (
    <div className='mx-auto max-w-5xl space-y-5'>
      <div className='flex items-center justify-between gap-3 border-b pb-4'>
        <div>
          <h1 className='font-semibold text-xl tracking-tight'>
            {t('routes.workspaces.kb.content.faq.edit.title', {
              title:
                faq.document.title ||
                t('routes.workspaces.kb.content.faq.edit.unnamedTitle'),
            })}
          </h1>
          <p className='mt-1 text-muted-foreground text-sm'>
            {t('routes.workspaces.kb.content.faq.edit.description')}
          </p>
        </div>
        <Button asChild variant='outline'>
          <a href={detailHref}>
            <ArrowLeft />
            {t('routes.workspaces.kb.content.faq.edit.backToDetail')}
          </a>
        </Button>
      </div>
      <FAQForm
        mode='edit'
        initialFAQ={faq}
        saveFAQ={saveFAQ}
        loadLatestFAQ={() => getFAQDocument(workspaceSlug, documentId)}
        onSaved={(saved) => {
          queryClient.setQueryData(
            faqDocumentQueryKey(workspaceSlug, documentId),
            saved
          )
          void invalidateFAQExperience(
            queryClient,
            workspaceSlug,
            kbId,
            documentId
          )
          toast.success(t('routes.workspaces.kb.content.faq.edit.savedToast'))
        }}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/faq/$documentId/edit'
)({
  loader: async ({ context, params }): Promise<FAQEditLoaderData> => {
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
        i18n.t('routes.workspaces.kb.content.faq.edit.unnamedTitle'),
    }
  },
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.faq.edit.breadcrumb',
      resolve: loaderDocumentName,
    },
  },
  component: FAQEditPage,
})
