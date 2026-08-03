import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { DocumentUploadForm } from '@/features/documents/components/document-upload-form'

const uploadSearchSchema = z.object({ parent: z.string().optional() })

function UploadFilePage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  const { parent } = Route.useSearch()
  return (
    <div className='mx-auto max-w-2xl space-y-5'>
      <div>
        <h2 className='font-semibold text-xl'>
          {t('routes.workspaces.kb.content.files.upload.title')}
        </h2>
        <p className='mt-1 text-muted-foreground text-sm'>
          {t('routes.workspaces.kb.content.files.upload.description')}
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>
            {t('routes.workspaces.kb.content.files.upload.cardTitle')}
          </CardTitle>
          <CardDescription>
            {t('routes.workspaces.kb.content.files.upload.cardDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DocumentUploadForm
            workspaceSlug={workspaceSlug}
            kbId={kbId}
            parentNodeId={parent}
          />
        </CardContent>
      </Card>
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/upload'
)({
  validateSearch: uploadSearchSchema,
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.files.upload.breadcrumb',
    },
  },
  component: UploadFilePage,
})
