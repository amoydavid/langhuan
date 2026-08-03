import { Clock3, FilePenLine, MessageCircleQuestion } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SafeMarkdown } from '@/components/safe-markdown'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { formatDateTime } from '@/lib/i18n/datetime'
import type { FAQDocument } from './schemas'

type FAQDetailProps = {
  faq: FAQDocument
  canEdit: boolean
  editHref: string
}

function formatCreatedAt(value: string) {
  return formatDateTime(value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

export function FAQDetail({ faq, canEdit, editHref }: FAQDetailProps) {
  const { t } = useTranslation()
  const statusMeta = {
    pending: { label: t('contentFaq.detail.statusPending'), tone: 'warning' },
    processing: {
      label: t('contentFaq.detail.statusProcessing'),
      tone: 'info',
    },
    ready: { label: t('contentFaq.detail.statusReady'), tone: 'success' },
    failed: { label: t('contentFaq.detail.statusFailed'), tone: 'danger' },
    deleting: {
      label: t('contentFaq.detail.statusDeleting'),
      tone: 'info',
    },
    deleted: { label: t('contentFaq.detail.statusDeleted'), tone: 'neutral' },
  } as const
  const status = statusMeta[faq.document.status]
  const isIndexing =
    faq.document.status === 'pending' || faq.document.status === 'processing'
  return (
    <article className='mx-auto max-w-5xl space-y-5'>
      <header className='flex flex-col justify-between gap-4 border-b pb-5 sm:flex-row sm:items-start'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-2'>
            <MessageCircleQuestion className='size-5 text-primary' />
            <h1 className='truncate font-semibold text-2xl tracking-tight'>
              {faq.document.title || t('contentFaq.detail.untitled')}
            </h1>
            <StatusBadge tone={status.tone}>{status.label}</StatusBadge>
          </div>
          <p className='mt-2 flex items-center gap-1.5 text-muted-foreground text-sm'>
            <Clock3 className='size-4' />
            {t('contentFaq.detail.revisionLabel', {
              no: faq.revision.revision_no,
            })}{' '}
            · {formatCreatedAt(faq.revision.created_at)}
          </p>
        </div>
        {canEdit && (
          <Button asChild>
            <a href={editHref}>
              <FilePenLine />
              {t('contentFaq.detail.editButton')}
            </a>
          </Button>
        )}
      </header>

      {isIndexing && (
        <Alert>
          <AlertTitle>{t('contentFaq.detail.indexingTitle')}</AlertTitle>
          <AlertDescription>
            {t('contentFaq.detail.indexingDescription')}
          </AlertDescription>
        </Alert>
      )}

      {faq.document.status === 'failed' && (
        <Alert variant='destructive'>
          <AlertTitle>{t('contentFaq.detail.failedTitle')}</AlertTitle>
          <AlertDescription>
            {faq.document.error_message ||
              t('contentFaq.detail.failedFallbackMessage')}
          </AlertDescription>
        </Alert>
      )}

      <div className='grid gap-5 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.4fr)]'>
        <Card>
          <CardHeader>
            <CardTitle className='text-base'>
              {t('contentFaq.detail.questionsTitle')}
            </CardTitle>
            <CardDescription>
              {t('contentFaq.detail.questionsCountDescription', {
                count: faq.questions.length,
              })}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ol className='space-y-2'>
              {faq.questions.map((question, index) => (
                <li
                  key={`${index + 1}-${question}`}
                  className='flex gap-3 rounded-lg border bg-muted/20 p-3 text-sm'
                >
                  <span className='flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 font-medium text-primary text-xs'>
                    {index + 1}
                  </span>
                  <span className='pt-0.5'>{question}</span>
                </li>
              ))}
            </ol>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className='text-base'>
              {t('contentFaq.detail.answerTitle')}
            </CardTitle>
            <CardDescription>
              {t('contentFaq.detail.answerDescription')}
            </CardDescription>
          </CardHeader>
          <CardContent data-testid='faq-answer'>
            <SafeMarkdown content={faq.answer} />
          </CardContent>
        </Card>
      </div>
    </article>
  )
}
