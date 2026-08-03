import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { createFAQDocument } from '@/features/content/faq-editor/api'
import { FAQForm } from '@/features/content/faq-editor/faq-form'
import {
  faqDocumentQueryKey,
  invalidateFAQExperience,
} from '@/features/content/faq-editor/queries'
import type {
  CreateFAQInput,
  FAQSaveInput,
} from '@/features/content/faq-editor/schemas'

function FAQCreatePage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  const navigate = Route.useNavigate()
  const queryClient = useQueryClient()
  const listHref = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/faq`

  async function saveFAQ(input: FAQSaveInput) {
    if ('base_revision_id' in input) {
      throw new Error('创建 FAQ 不能包含 base_revision_id')
    }
    return createFAQDocument(workspaceSlug, kbId, input as CreateFAQInput)
  }

  return (
    <div className='mx-auto max-w-5xl space-y-5'>
      <div className='flex items-center justify-between gap-3 border-b pb-4'>
        <div>
          <h1 className='font-semibold text-xl tracking-tight'>
            {t('routes.workspaces.kb.content.faq.new.title')}
          </h1>
          <p className='mt-1 text-muted-foreground text-sm'>
            {t('routes.workspaces.kb.content.faq.new.description')}
          </p>
        </div>
        <Button asChild variant='outline'>
          <a href={listHref}>
            <ArrowLeft />
            {t('routes.workspaces.kb.content.faq.new.backToList')}
          </a>
        </Button>
      </div>
      <FAQForm
        mode='create'
        saveFAQ={saveFAQ}
        onSaved={(faq) => {
          queryClient.setQueryData(
            faqDocumentQueryKey(workspaceSlug, faq.document.id),
            faq
          )
          void invalidateFAQExperience(
            queryClient,
            workspaceSlug,
            kbId,
            faq.document.id
          )
          void navigate({
            to: `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/faq/${encodeURIComponent(faq.document.id)}`,
          })
        }}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/faq/new'
)({
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.faq.new.breadcrumb',
    },
  },
  component: FAQCreatePage,
})
