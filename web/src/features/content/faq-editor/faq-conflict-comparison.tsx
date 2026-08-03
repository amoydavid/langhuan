import { useTranslation } from 'react-i18next'
import type { FAQDocument, FAQFormValues } from './schemas'

type FAQConflictComparisonProps = {
  draft: FAQFormValues
  latest: FAQDocument
}

export function FAQConflictComparison({
  draft,
  latest,
}: FAQConflictComparisonProps) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-3 lg:grid-cols-2'>
      <div className='rounded-xl border bg-muted/20 p-4'>
        <h3 className='font-medium text-sm'>
          {t('contentFaq.conflictComparison.yourVersion')}
        </h3>
        <p className='mt-3 text-muted-foreground text-xs'>
          {t('contentFaq.conflictComparison.questionsLabel')}
        </p>
        <ol className='mt-1 list-decimal space-y-1 ps-5 text-sm'>
          {draft.questions.map((question, index) => (
            <li key={`draft-question-${index + 1}`}>{question}</li>
          ))}
        </ol>
        <p className='mt-4 text-muted-foreground text-xs'>
          {t('contentFaq.conflictComparison.answerLabel')}
        </p>
        <pre className='mt-1 whitespace-pre-wrap font-sans text-sm'>
          {draft.answer}
        </pre>
      </div>
      <div className='rounded-xl border bg-muted/20 p-4'>
        <h3 className='font-medium text-sm'>
          {t('contentFaq.conflictComparison.latestVersion')}
        </h3>
        <p className='mt-3 text-muted-foreground text-xs'>
          {t('contentFaq.conflictComparison.questionsLabel')}
        </p>
        <ol className='mt-1 list-decimal space-y-1 ps-5 text-sm'>
          {latest.questions.map((question, index) => (
            <li key={`latest-question-${index + 1}`}>{question}</li>
          ))}
        </ol>
        <p className='mt-4 text-muted-foreground text-xs'>
          {t('contentFaq.conflictComparison.answerLabel')}
        </p>
        <pre className='mt-1 whitespace-pre-wrap font-sans text-sm'>
          {latest.answer}
        </pre>
      </div>
    </div>
  )
}
