import { zodResolver } from '@hookform/resolvers/zod'
import { Copy, Database, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { IndexGeneration } from '@/features/index-generations/types'
import type { KnowledgeBase } from '@/features/knowledge-bases/types'
import type { UpdateKnowledgeBaseBasicsInput } from '@/features/knowledge-bases/workbench/types'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'

const basicsSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, { error: () => i18n.t('knowledgeBases.settings.nameRequired') }),
  description: z.string().trim(),
})

type BasicsValues = z.infer<typeof basicsSchema>

type KnowledgeBaseSettingsProps = {
  knowledgeBase: KnowledgeBase
  activeGeneration?: IndexGeneration
  canManage: boolean
  saveBasics: (input: UpdateKnowledgeBaseBasicsInput) => Promise<KnowledgeBase>
  copyText: (text: string) => Promise<void>
  buildIndexHref: string
}

function configValue(config: Record<string, unknown>, key: string) {
  const value = config[key]
  return typeof value === 'string' || typeof value === 'number' ? value : '—'
}

export function KnowledgeBaseSettings({
  knowledgeBase,
  activeGeneration,
  canManage,
  saveBasics,
  copyText,
  buildIndexHref,
}: KnowledgeBaseSettingsProps) {
  const { t } = useTranslation()
  const [submitError, setSubmitError] = useState<string>()
  const [copied, setCopied] = useState(false)
  const form = useForm<BasicsValues>({
    resolver: zodResolver(basicsSchema),
    defaultValues: {
      name: knowledgeBase.name,
      description: knowledgeBase.description,
    },
  })

  async function submit(values: BasicsValues) {
    setSubmitError(undefined)
    try {
      const updated = await saveBasics({
        name: values.name.trim(),
        description: values.description.trim(),
      })
      form.reset({ name: updated.name, description: updated.description })
    } catch (error) {
      setSubmitError(parseApiError(error).message)
    }
  }

  async function copyDiagnostics() {
    const diagnostics = {
      knowledge_base_id: knowledgeBase.id,
      workspace_id: knowledgeBase.workspace_id,
      active_generation_id:
        activeGeneration?.id ?? knowledgeBase.active_index_generation_id,
      generation_config_hash: activeGeneration?.config_hash,
      content_version: knowledgeBase.content_version,
      updated_at: knowledgeBase.updated_at,
    }
    await copyText(JSON.stringify(diagnostics, null, 2))
    setCopied(true)
  }

  return (
    <div className='grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,0.8fr)]'>
      <Card>
        <CardHeader>
          <CardTitle>{t('knowledgeBases.settings.basicsTitle')}</CardTitle>
          <CardDescription>
            {t('knowledgeBases.settings.basicsDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {!canManage && (
            <Alert className='mb-5'>
              <AlertTitle>
                {t('knowledgeBases.settings.readOnlyTitle')}
              </AlertTitle>
              <AlertDescription>
                {t('knowledgeBases.settings.readOnlyDescription')}
              </AlertDescription>
            </Alert>
          )}
          <Form {...form}>
            <form onSubmit={form.handleSubmit(submit)} className='space-y-4'>
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('knowledgeBases.settings.nameLabel')}
                    </FormLabel>
                    <FormControl>
                      <Input {...field} disabled={!canManage} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='description'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('knowledgeBases.settings.descriptionLabel')}
                    </FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={5} disabled={!canManage} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {submitError && (
                <p role='alert' className='text-destructive text-sm'>
                  {submitError}
                </p>
              )}
              {canManage && (
                <Button type='submit' disabled={form.formState.isSubmitting}>
                  {form.formState.isSubmitting && (
                    <Loader2 className='animate-spin' />
                  )}
                  {t('knowledgeBases.settings.saveBasicsButton')}
                </Button>
              )}
            </form>
          </Form>
        </CardContent>
      </Card>

      <div className='space-y-5'>
        <Card>
          <CardHeader>
            <CardTitle>{t('knowledgeBases.settings.configTitle')}</CardTitle>
            <CardDescription>
              {t('knowledgeBases.settings.configDescription')}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-4 text-sm'>
            {activeGeneration ? (
              <dl className='grid gap-3 sm:grid-cols-2 xl:grid-cols-1 2xl:grid-cols-2'>
                <div>
                  <dt className='text-muted-foreground text-xs'>
                    {t('knowledgeBases.settings.modelLabel')}
                  </dt>
                  <dd className='mt-1 font-medium'>
                    {activeGeneration.model_name}
                  </dd>
                </div>
                <div>
                  <dt className='text-muted-foreground text-xs'>
                    {t('knowledgeBases.settings.dimensionLabel')}
                  </dt>
                  <dd className='mt-1 font-medium'>
                    {activeGeneration.embedding_dimension}
                  </dd>
                </div>
                <div>
                  <dt className='text-muted-foreground text-xs'>
                    {t('knowledgeBases.settings.chunkingLabel')}
                  </dt>
                  <dd className='mt-1 font-medium'>
                    {configValue(
                      activeGeneration.chunking_config,
                      'chunk_size'
                    )}
                    /
                    {configValue(
                      activeGeneration.chunking_config,
                      'chunk_overlap'
                    )}
                  </dd>
                </div>
                <div>
                  <dt className='text-muted-foreground text-xs'>
                    {t('knowledgeBases.settings.retrievalLabel')}
                  </dt>
                  <dd className='mt-1 font-medium'>
                    {configValue(
                      activeGeneration.retrieval_config,
                      'vector_top_k'
                    )}{' '}
                    +{' '}
                    {configValue(
                      activeGeneration.retrieval_config,
                      'keyword_top_k'
                    )}{' '}
                    →{' '}
                    {configValue(
                      activeGeneration.retrieval_config,
                      'final_top_k'
                    )}
                  </dd>
                </div>
              </dl>
            ) : (
              <p className='text-muted-foreground'>
                {t('knowledgeBases.settings.noActiveVersion')}
              </p>
            )}
            {canManage && (
              <Button asChild variant='outline'>
                <a href={buildIndexHref}>
                  <Database />
                  {t('knowledgeBases.settings.buildIndexButton')}
                </a>
              </Button>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>
              {t('knowledgeBases.settings.diagnosticsTitle')}
            </CardTitle>
            <CardDescription>
              {t('knowledgeBases.settings.diagnosticsDescription')}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button
              type='button'
              variant='outline'
              onClick={() => void copyDiagnostics()}
            >
              <Copy />
              {copied
                ? t('knowledgeBases.settings.copiedDiagnostics')
                : t('knowledgeBases.settings.copyDiagnostics')}
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
