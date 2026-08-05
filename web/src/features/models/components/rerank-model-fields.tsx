import type { UseFormRegister } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { RerankModelFormValues } from '../schemas/common'

type RerankModelFieldsProps = {
  register: UseFormRegister<RerankModelFormValues>
  errors: {
    max_documents?: { message?: string }
    max_query_chars?: { message?: string }
    max_document_chars?: { message?: string }
  }
}

export function RerankModelFields({
  register,
  errors,
}: RerankModelFieldsProps) {
  const { t } = useTranslation()
  return (
    <>
      <div className='grid gap-2'>
        <Label
          htmlFor='rerank-max-documents'
          className={errors.max_documents ? 'text-destructive' : undefined}
        >
          {t('models.modelForm.maxDocumentsLabel')}
        </Label>
        <Input
          id='rerank-max-documents'
          type='number'
          min={50}
          max={200}
          aria-invalid={Boolean(errors.max_documents)}
          {...register('max_documents', { valueAsNumber: true })}
        />
        {errors.max_documents?.message && (
          <p className='text-destructive text-sm' role='alert'>
            {errors.max_documents.message}
          </p>
        )}
      </div>
      <div className='grid gap-2'>
        <Label
          htmlFor='rerank-max-query-chars'
          className={errors.max_query_chars ? 'text-destructive' : undefined}
        >
          {t('models.modelForm.maxQueryCharsLabel')}
        </Label>
        <Input
          id='rerank-max-query-chars'
          type='number'
          min={256}
          max={4096}
          aria-invalid={Boolean(errors.max_query_chars)}
          {...register('max_query_chars', { valueAsNumber: true })}
        />
        {errors.max_query_chars?.message && (
          <p className='text-destructive text-sm' role='alert'>
            {errors.max_query_chars.message}
          </p>
        )}
      </div>
      <div className='grid gap-2'>
        <Label
          htmlFor='rerank-max-document-chars'
          className={errors.max_document_chars ? 'text-destructive' : undefined}
        >
          {t('models.modelForm.maxDocumentCharsLabel')}
        </Label>
        <Input
          id='rerank-max-document-chars'
          type='number'
          min={512}
          max={32768}
          aria-invalid={Boolean(errors.max_document_chars)}
          {...register('max_document_chars', { valueAsNumber: true })}
        />
        {errors.max_document_chars?.message && (
          <p className='text-destructive text-sm' role='alert'>
            {errors.max_document_chars.message}
          </p>
        )}
      </div>
    </>
  )
}
