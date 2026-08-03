import { zodResolver } from '@hookform/resolvers/zod'
import {
  ArrowLeft,
  ArrowRight,
  CircleHelp,
  Database,
  Loader2,
} from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { parseApiError } from '@/lib/api/error'
import {
  type GenerationFormValues,
  generationFormDefaults,
  generationFormSchema,
  toCreateGenerationInput,
} from './generation-form-schema'
import type { CreateIndexGenerationInput, IndexGeneration } from './types'

// 面向非技术人员的常用全文检索配置（value 为 PostgreSQL text search configuration 名）
const ftsConfigOptions = ['zhparser', 'simple', 'english'] as const
const customFTSConfigOption = '__custom__'

function isPresetFTSConfig(value: string) {
  return ftsConfigOptions.some((option) => option === value)
}

type SelectableGenerationModel = {
  id: string
  displayName: string
  dimensions: number
}

type GenerationFormProps = {
  models: SelectableGenerationModel[]
  baseGeneration: IndexGeneration
  createGeneration: (
    input: CreateIndexGenerationInput
  ) => Promise<IndexGeneration>
  onCreated?: (generation: IndexGeneration) => void
  onCancel?: () => void
}

// FormLabel 旁的小问号图标，悬停显示面向非技术人员的字段说明。
function HintLabel({ label, hint }: { label: string; hint: string }) {
  const { t } = useTranslation()

  return (
    <div className='flex items-center gap-1.5'>
      <FormLabel>{label}</FormLabel>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type='button'
            aria-label={t('indexGenerations.generationForm.hintAction', {
              label,
            })}
            className='inline-flex min-h-[44px] min-w-[44px] shrink-0 items-center justify-center text-muted-foreground transition-colors hover:text-foreground'
          >
            <CircleHelp className='size-3.5' />
          </button>
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-64 text-xs leading-relaxed'>
          {hint}
        </TooltipContent>
      </Tooltip>
    </div>
  )
}

export function GenerationForm({
  models,
  baseGeneration,
  createGeneration,
  onCreated,
  onCancel,
}: GenerationFormProps) {
  const { t } = useTranslation()
  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [submitError, setSubmitError] = useState<string>()
  const form = useForm<GenerationFormValues>({
    resolver: zodResolver(generationFormSchema),
    defaultValues: generationFormDefaults(baseGeneration),
  })

  const steps = [
    {
      number: 1,
      label: t('indexGenerations.generationForm.steps.embeddingModel'),
    },
    {
      number: 2,
      label: t('indexGenerations.generationForm.steps.chunkConfig'),
    },
    {
      number: 3,
      label: t('indexGenerations.generationForm.steps.retrievalConfig'),
    },
  ] as const

  const topKFields = [
    {
      name: 'vector_top_k',
      label: t('indexGenerations.generationForm.retrievalStep.vectorTopKLabel'),
      hint: t('indexGenerations.generationForm.retrievalStep.vectorTopKHint'),
      max: 200,
    },
    {
      name: 'keyword_top_k',
      label: t(
        'indexGenerations.generationForm.retrievalStep.keywordTopKLabel'
      ),
      hint: t('indexGenerations.generationForm.retrievalStep.keywordTopKHint'),
      max: 200,
    },
    {
      name: 'final_top_k',
      label: t('indexGenerations.generationForm.retrievalStep.finalTopKLabel'),
      hint: t('indexGenerations.generationForm.retrievalStep.finalTopKHint'),
      max: 50,
    },
    {
      name: 'rrf_k',
      label: t('indexGenerations.generationForm.retrievalStep.rrfKLabel'),
      hint: t('indexGenerations.generationForm.retrievalStep.rrfKHint'),
      max: undefined,
    },
  ] as const

  async function nextStep(fields: (keyof GenerationFormValues)[], next: 2 | 3) {
    const valid = await form.trigger(fields)
    if (valid) setStep(next)
  }

  async function submit(values: GenerationFormValues) {
    setSubmitError(undefined)
    try {
      const generation = await createGeneration(toCreateGenerationInput(values))
      form.reset(values)
      onCreated?.(generation)
    } catch (error) {
      setSubmitError(parseApiError(error).message)
    }
  }

  return (
    <TooltipProvider>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(submit)} className='space-y-5'>
          <ol
            className='grid grid-cols-3 gap-2 text-sm'
            aria-label={t('indexGenerations.generationForm.ariaLabel')}
          >
            {steps.map(({ number, label }) => (
              <li
                key={number}
                className={
                  step === number
                    ? 'rounded-lg bg-primary/10 px-3 py-2 font-medium text-primary'
                    : 'rounded-lg bg-muted/40 px-3 py-2 text-muted-foreground'
                }
              >
                {label}
              </li>
            ))}
          </ol>

          {step === 1 && (
            <section
              className='space-y-4'
              aria-labelledby='generation-step-model'
            >
              <div>
                <h2
                  id='generation-step-model'
                  className='font-semibold text-lg'
                >
                  {t('indexGenerations.generationForm.stepHeadings.model')}
                </h2>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t('indexGenerations.generationForm.modelStep.description')}
                </p>
              </div>
              <FormField
                control={form.control}
                name='embedding_model_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t(
                        'indexGenerations.generationForm.steps.embeddingModel'
                      )}
                    </FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue
                            placeholder={t(
                              'indexGenerations.generationForm.modelStep.selectPlaceholder'
                            )}
                          />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {models.map((model) => (
                          <SelectItem key={model.id} value={model.id}>
                            {model.displayName} ·{' '}
                            {t(
                              'indexGenerations.generationForm.modelStep.dimensions',
                              {
                                count: model.dimensions,
                              }
                            )}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t(
                        'indexGenerations.generationForm.modelStep.availableModelsDescription'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <div className='flex justify-between'>
                {onCancel ? (
                  <Button type='button' variant='outline' onClick={onCancel}>
                    {t('indexGenerations.generationForm.cancel')}
                  </Button>
                ) : (
                  <span />
                )}
                <Button
                  type='button'
                  onClick={() => void nextStep(['embedding_model_id'], 2)}
                >
                  {t('indexGenerations.generationForm.nextStepChunk')}
                  <ArrowRight />
                </Button>
              </div>
            </section>
          )}

          {step === 2 && (
            <section
              className='space-y-4'
              aria-labelledby='generation-step-chunk'
            >
              <div>
                <h2
                  id='generation-step-chunk'
                  className='font-semibold text-lg'
                >
                  {t('indexGenerations.generationForm.stepHeadings.chunk')}
                </h2>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t('indexGenerations.generationForm.chunkStep.description')}
                </p>
              </div>
              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='chunk_size'
                  render={({ field }) => (
                    <FormItem>
                      <HintLabel
                        label={t(
                          'indexGenerations.generationForm.chunkStep.sizeLabel'
                        )}
                        hint={t(
                          'indexGenerations.generationForm.chunkStep.sizeHint'
                        )}
                      />
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='chunk_overlap'
                  render={({ field }) => (
                    <FormItem>
                      <HintLabel
                        label={t(
                          'indexGenerations.generationForm.chunkStep.overlapLabel'
                        )}
                        hint={t(
                          'indexGenerations.generationForm.chunkStep.overlapHint'
                        )}
                      />
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <div className='flex justify-between'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setStep(1)}
                >
                  <ArrowLeft />
                  {t('indexGenerations.generationForm.previousStep')}
                </Button>
                <Button
                  type='button'
                  onClick={() =>
                    void nextStep(['chunk_size', 'chunk_overlap'], 3)
                  }
                >
                  {t('indexGenerations.generationForm.nextStepRetrieval')}
                  <ArrowRight />
                </Button>
              </div>
            </section>
          )}

          {step === 3 && (
            <section
              className='space-y-4'
              aria-labelledby='generation-step-retrieval'
            >
              <div>
                <h2
                  id='generation-step-retrieval'
                  className='font-semibold text-lg'
                >
                  {t('indexGenerations.generationForm.stepHeadings.retrieval')}
                </h2>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t(
                    'indexGenerations.generationForm.retrievalStep.description'
                  )}
                </p>
              </div>
              <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
                <FormField
                  control={form.control}
                  name='fts_config'
                  render={({ field }) => {
                    const usesPreset = isPresetFTSConfig(field.value)
                    return (
                      <FormItem>
                        <HintLabel
                          label={t(
                            'indexGenerations.generationForm.retrievalStep.ftsLabel'
                          )}
                          hint={t(
                            'indexGenerations.generationForm.retrievalStep.ftsHint'
                          )}
                        />
                        <Select
                          value={
                            usesPreset ? field.value : customFTSConfigOption
                          }
                          onValueChange={(value) => {
                            if (value === customFTSConfigOption) {
                              if (usesPreset) field.onChange('')
                              return
                            }
                            field.onChange(value)
                          }}
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue
                                placeholder={t(
                                  'indexGenerations.generationForm.retrievalStep.ftsSelectPlaceholder'
                                )}
                              />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent>
                            {ftsConfigOptions.map((option) => (
                              <SelectItem key={option} value={option}>
                                {t(
                                  `indexGenerations.generationForm.retrievalStep.ftsOptions.${option}`
                                )}
                              </SelectItem>
                            ))}
                            <SelectItem value={customFTSConfigOption}>
                              {t(
                                'indexGenerations.generationForm.retrievalStep.ftsOptions.custom'
                              )}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                        {!usesPreset && (
                          <div className='space-y-2'>
                            <FormLabel htmlFor='custom-fts-config'>
                              {t(
                                'indexGenerations.generationForm.retrievalStep.customFtsLabel'
                              )}
                            </FormLabel>
                            <Input
                              id='custom-fts-config'
                              value={field.value}
                              onChange={field.onChange}
                              placeholder={t(
                                'indexGenerations.generationForm.retrievalStep.customFtsPlaceholder'
                              )}
                            />
                            <FormDescription>
                              {t(
                                'indexGenerations.generationForm.retrievalStep.customFtsDescription'
                              )}
                            </FormDescription>
                          </div>
                        )}
                        <FormMessage />
                      </FormItem>
                    )
                  }}
                />
                {topKFields.map(({ name, label, hint, max }) => (
                  <FormField
                    key={name}
                    control={form.control}
                    name={name}
                    render={({ field }) => (
                      <FormItem>
                        <HintLabel label={label} hint={hint} />
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={max}
                            {...field}
                            onChange={(event) =>
                              field.onChange(event.target.valueAsNumber)
                            }
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                ))}
              </div>
              {submitError && (
                <p role='alert' className='text-destructive text-sm'>
                  {submitError}
                </p>
              )}
              <div className='flex justify-between'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setStep(2)}
                >
                  <ArrowLeft />
                  {t('indexGenerations.generationForm.previousStep')}
                </Button>
                <Button type='submit' disabled={form.formState.isSubmitting}>
                  {form.formState.isSubmitting ? (
                    <Loader2 className='animate-spin' />
                  ) : (
                    <Database />
                  )}
                  {t('indexGenerations.generationForm.submit')}
                </Button>
              </div>
            </section>
          )}
        </form>
      </Form>
    </TooltipProvider>
  )
}
