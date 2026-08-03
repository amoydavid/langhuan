import { zodResolver } from '@hookform/resolvers/zod'
import {
  ArrowDown,
  ArrowUp,
  Columns2,
  Eye,
  FilePenLine,
  Loader2,
  Plus,
  Trash2,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { SafeMarkdown } from '@/components/safe-markdown'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { parseApiError } from '@/lib/api/error'
import { cn } from '@/lib/utils'
import { FAQConflictComparison } from './faq-conflict-comparison'
import {
  type FAQDocument,
  type FAQFormValues,
  type FAQSaveInput,
  faqFormSchema,
} from './schemas'

type FAQFormProps = {
  mode: 'create' | 'edit'
  initialFAQ?: FAQDocument
  saveFAQ: (input: FAQSaveInput) => Promise<FAQDocument>
  loadLatestFAQ?: () => Promise<FAQDocument>
  onSaved?: (faq: FAQDocument) => void
  onDirtyChange?: (dirty: boolean) => void
}

type EditorView = 'edit' | 'preview' | 'split'

function initialValues(faq?: FAQDocument): FAQFormValues {
  return {
    title: faq?.document.title ?? '',
    questions: faq?.questions.length ? faq.questions : [''],
    answer: faq?.answer ?? '',
  }
}

export function FAQForm({
  mode,
  initialFAQ,
  saveFAQ,
  loadLatestFAQ,
  onSaved,
  onDirtyChange,
}: FAQFormProps) {
  const { t } = useTranslation()
  const [view, setView] = useState<EditorView>('edit')
  const [latestFAQ, setLatestFAQ] = useState<FAQDocument>()
  const [submitError, setSubmitError] =
    useState<ReturnType<typeof parseApiError>>()
  const form = useForm<FAQFormValues>({
    resolver: zodResolver(faqFormSchema),
    defaultValues: initialValues(initialFAQ),
  })
  const questionValues = form.watch('questions')

  function setQuestions(next: string[]) {
    form.setValue('questions', next, {
      shouldDirty: true,
      shouldTouch: true,
      shouldValidate: true,
    })
  }

  function moveQuestion(from: number, to: number) {
    if (to < 0 || to >= questionValues.length) return
    const next = [...questionValues]
    const [question] = next.splice(from, 1)
    if (question === undefined) return
    next.splice(to, 0, question)
    setQuestions(next)
  }

  useEffect(() => {
    onDirtyChange?.(form.formState.isDirty)
    if (!form.formState.isDirty) return
    function preventDirtyLeave(event: BeforeUnloadEvent) {
      event.preventDefault()
    }
    window.addEventListener('beforeunload', preventDirtyLeave)
    return () => window.removeEventListener('beforeunload', preventDirtyLeave)
  }, [form.formState.isDirty, onDirtyChange])

  async function submitWithBase(
    values: FAQFormValues,
    baseRevisionId?: string
  ) {
    setSubmitError(undefined)
    try {
      const input: FAQSaveInput =
        mode === 'create'
          ? values
          : {
              base_revision_id: baseRevisionId ?? initialFAQ?.revision.id ?? '',
              questions: values.questions,
              answer: values.answer,
            }
      const saved = await saveFAQ(input)
      setLatestFAQ(undefined)
      form.reset({
        title: saved.document.title,
        questions: saved.questions,
        answer: saved.answer,
      })
      onSaved?.(saved)
    } catch (error) {
      const apiError = parseApiError(error)
      setSubmitError(apiError)
      if (apiError.code !== 'revision_conflict' || !loadLatestFAQ) return
      try {
        setLatestFAQ(await loadLatestFAQ())
      } catch (latestError) {
        setSubmitError(parseApiError(latestError))
      }
    }
  }

  const answer = form.watch('answer')
  const currentDraft = form.getValues()
  const viewOptions = [
    { value: 'edit', label: t('contentFaq.form.viewEdit'), icon: FilePenLine },
    {
      value: 'preview',
      label: t('contentFaq.form.viewPreview'),
      icon: Eye,
    },
    { value: 'split', label: t('contentFaq.form.viewSplit'), icon: Columns2 },
  ] as const
  return (
    <Form {...form}>
      <form
        className='space-y-6'
        onSubmit={form.handleSubmit((values) => submitWithBase(values))}
      >
        {submitError && (
          <Alert variant='destructive' role='alert'>
            <AlertTitle>
              {submitError.code === 'revision_conflict'
                ? t('contentFaq.form.conflictTitle')
                : t('contentFaq.form.saveFailedTitle')}
            </AlertTitle>
            <AlertDescription>
              {submitError.code === 'revision_conflict'
                ? t('contentFaq.form.conflictDescription')
                : submitError.message}
            </AlertDescription>
          </Alert>
        )}

        {submitError?.code === 'revision_conflict' && latestFAQ && (
          <div className='space-y-3 rounded-xl border border-destructive/30 bg-destructive/5 p-4'>
            <FAQConflictComparison draft={currentDraft} latest={latestFAQ} />
            <div className='flex justify-end'>
              <Button
                type='button'
                variant='outline'
                onClick={async () => {
                  const valid = await form.trigger()
                  if (valid) {
                    await submitWithBase(
                      form.getValues(),
                      latestFAQ.revision.id
                    )
                  }
                }}
              >
                {t('contentFaq.form.retryOnLatest')}
              </Button>
            </div>
          </div>
        )}

        {mode === 'create' && (
          <FormField
            control={form.control}
            name='title'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('contentFaq.form.titleLabel')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    autoFocus
                    placeholder={t('contentFaq.form.titlePlaceholder')}
                  />
                </FormControl>
                <FormDescription>
                  {t('contentFaq.form.titleDescription')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        <section className='space-y-3' aria-labelledby='faq-questions-title'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div>
              <h2 id='faq-questions-title' className='font-medium text-sm'>
                {t('contentFaq.form.questionsTitle')}
              </h2>
              <p className='mt-1 text-muted-foreground text-xs'>
                {t('contentFaq.form.questionsHint')}
              </p>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setQuestions([...questionValues, ''])}
            >
              <Plus />
              {t('contentFaq.form.addQuestion')}
            </Button>
          </div>
          <div className='space-y-3'>
            {questionValues.map((_, index) => (
              <FormField
                key={`faq-question-${index + 1}`}
                control={form.control}
                name={`questions.${index}`}
                render={({ field }) => (
                  <FormItem className='rounded-xl border bg-muted/10 p-3'>
                    <div className='flex items-center justify-between gap-2'>
                      <FormLabel>
                        {t('contentFaq.form.questionLabel', {
                          index: index + 1,
                        })}
                      </FormLabel>
                      <div className='flex items-center gap-1'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='size-8'
                          disabled={index === 0}
                          aria-label={t(
                            'contentFaq.form.moveQuestionUpAriaLabel',
                            {
                              index: index + 1,
                            }
                          )}
                          onClick={() => moveQuestion(index, index - 1)}
                        >
                          <ArrowUp />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='size-8'
                          disabled={index === questionValues.length - 1}
                          aria-label={t(
                            'contentFaq.form.moveQuestionDownAriaLabel',
                            {
                              index: index + 1,
                            }
                          )}
                          onClick={() => moveQuestion(index, index + 1)}
                        >
                          <ArrowDown />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='size-8'
                          aria-label={t(
                            'contentFaq.form.deleteQuestionAriaLabel',
                            {
                              index: index + 1,
                            }
                          )}
                          onClick={() =>
                            setQuestions(
                              questionValues.filter(
                                (_, questionIndex) => questionIndex !== index
                              )
                            )
                          }
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </div>
                    <FormControl>
                      <Input
                        {...field}
                        placeholder={t('contentFaq.form.questionPlaceholder')}
                        onKeyDown={(event) => {
                          if (!event.altKey) return
                          if (event.key === 'ArrowUp' && index > 0) {
                            event.preventDefault()
                            moveQuestion(index, index - 1)
                          }
                          if (
                            event.key === 'ArrowDown' &&
                            index < questionValues.length - 1
                          ) {
                            event.preventDefault()
                            moveQuestion(index, index + 1)
                          }
                        }}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
          </div>
        </section>

        <Separator />

        <FormField
          control={form.control}
          name='answer'
          render={({ field }) => (
            <FormItem>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <FormLabel>{t('contentFaq.form.answerLabel')}</FormLabel>
                <fieldset
                  className='flex rounded-lg border p-1'
                  aria-label={t('contentFaq.form.answerViewAriaLabel')}
                >
                  {viewOptions.map(({ value, label, icon: Icon }) => (
                    <Button
                      key={value}
                      type='button'
                      size='sm'
                      variant='ghost'
                      aria-pressed={view === value}
                      className={cn(view === value && 'bg-muted')}
                      onClick={() => setView(value)}
                    >
                      <Icon />
                      {label}
                    </Button>
                  ))}
                </fieldset>
              </div>
              <div
                className={cn(
                  'grid gap-3',
                  view === 'split' && 'lg:grid-cols-2'
                )}
              >
                {view !== 'preview' && (
                  <FormControl>
                    <Textarea
                      {...field}
                      rows={16}
                      placeholder={t('contentFaq.form.answerPlaceholder')}
                    />
                  </FormControl>
                )}
                {view !== 'edit' && (
                  <div
                    data-testid='faq-answer-preview'
                    className='min-h-64 rounded-xl border bg-card p-5'
                  >
                    {answer.trim() ? (
                      <SafeMarkdown content={answer} />
                    ) : (
                      <p className='text-muted-foreground text-sm'>
                        {t('contentFaq.form.previewEmptyHint')}
                      </p>
                    )}
                  </div>
                )}
              </div>
              <FormDescription>
                {t('contentFaq.form.answerDescription')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className='flex justify-end'>
          <Button type='submit' disabled={form.formState.isSubmitting}>
            {form.formState.isSubmitting && (
              <Loader2 className='animate-spin' />
            )}
            {mode === 'create'
              ? t('contentFaq.form.saveCreate')
              : t('contentFaq.form.saveNewVersion')}
          </Button>
        </div>
      </form>
    </Form>
  )
}
