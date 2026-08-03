import { z } from 'zod'
import { documentResponseSchema } from '@/features/documents/schemas'
import i18n from '@/lib/i18n'

function normalizedQuestion(value: string) {
  return value.trim().toLocaleLowerCase().split(/\s+/u).join(' ')
}

export const faqFormSchema = z
  .object({
    title: z
      .string()
      .trim()
      .min(1, { message: i18n.t('contentFaq.schema.titleRequired') }),
    questions: z
      .array(
        z
          .string()
          .trim()
          .min(1, {
            message: i18n.t('contentFaq.schema.questionRequired'),
          })
      )
      .min(1, { message: i18n.t('contentFaq.schema.questionsMin') }),
    answer: z
      .string()
      .trim()
      .min(1, { message: i18n.t('contentFaq.schema.answerRequired') }),
  })
  .superRefine((value, context) => {
    const seen = new Set<string>()
    value.questions.forEach((question, index) => {
      const normalized = normalizedQuestion(question)
      if (!normalized || !seen.has(normalized)) {
        if (normalized) seen.add(normalized)
        return
      }
      context.addIssue({
        code: 'custom',
        message: i18n.t('contentFaq.schema.questionsDuplicate'),
        path: ['questions', index],
      })
    })
  })

export const faqRevisionSchema = z.object({
  id: z.uuid(),
  revision_no: z.number().int().positive(),
  status: z.enum(['pending', 'parsing', 'ready', 'failed']),
  created_at: z.string(),
})

const faqJobSchema = z.object({
  status: z.enum([
    'pending',
    'queued',
    'running',
    'completed',
    'succeeded',
    'failed',
    'cancelled',
  ]),
})

export const faqDocumentSchema = z.object({
  document: documentResponseSchema,
  revision: faqRevisionSchema,
  questions: z.array(z.string()),
  answer: z.string(),
  job: faqJobSchema.optional(),
})

export type FAQFormValues = z.infer<typeof faqFormSchema>
export type FAQDocument = z.infer<typeof faqDocumentSchema>
export type CreateFAQInput = FAQFormValues
export type UpdateFAQInput = Omit<FAQFormValues, 'title'> & {
  base_revision_id: string
}
export type FAQSaveInput = CreateFAQInput | UpdateFAQInput
