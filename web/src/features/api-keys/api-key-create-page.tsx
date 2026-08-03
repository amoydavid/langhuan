import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { APIKeyCreateForm } from './components/api-key-create-form'
import type { APIKey } from './types'

type APIKeyCreatePageProps = {
  // 由路由 loader 预取的知识库列表，供创建表单多选。
  knowledgeBases: Pick<APIKey['knowledge_bases'][number], 'id' | 'name'>[]
}

export function APIKeyCreatePage({ knowledgeBases }: APIKeyCreatePageProps) {
  const { t } = useTranslation()
  const { workspaceSlug } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/api-keys/new',
  })

  return (
    <div className='mx-auto max-w-3xl space-y-6'>
      <div>
        <p className='page-eyebrow'>{t('apiKeys.createPage.eyebrow')}</p>
        <h1 className='font-semibold text-2xl tracking-tight'>
          {t('apiKeys.createPage.title')}
        </h1>
        <p className='mt-2 text-muted-foreground'>
          {t('apiKeys.createPage.description')}
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{t('apiKeys.createPage.configTitle')}</CardTitle>
          <CardDescription>
            {t('apiKeys.createPage.configDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <APIKeyCreateForm
            workspaceSlug={workspaceSlug}
            knowledgeBases={knowledgeBases}
          />
        </CardContent>
      </Card>
    </div>
  )
}
