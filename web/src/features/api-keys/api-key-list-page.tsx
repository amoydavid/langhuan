import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { APIKeyTable } from './components/api-key-table'
import { apiKeysQueryOptions } from './queries'

export function APIKeyListPage() {
  const { t } = useTranslation()
  const { workspaceSlug } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/api-keys/',
  })
  const { data, isPending } = useQuery(apiKeysQueryOptions(workspaceSlug))
  const items = data?.items ?? []

  return (
    <div className='space-y-6'>
      <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
        <div>
          <p className='page-eyebrow'>{t('apiKeys.listPage.eyebrow')}</p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('apiKeys.listPage.title')}
          </h1>
          <p className='mt-2 max-w-2xl text-muted-foreground'>
            {t('apiKeys.listPage.description')}
          </p>
        </div>
        <Button asChild>
          <Link
            to='/workspaces/$workspaceSlug/api-keys/new'
            params={{ workspaceSlug }}
          >
            <Plus />
            {t('apiKeys.listPage.createButton')}
          </Link>
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('apiKeys.listPage.listTitle')}</CardTitle>
          <CardDescription>
            {t('apiKeys.listPage.listDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <div className='rounded-lg border border-dashed p-10 text-center text-muted-foreground text-sm'>
              {t('apiKeys.listPage.loading')}
            </div>
          ) : (
            <APIKeyTable workspaceSlug={workspaceSlug} items={items} />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
