import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowRight, BookOpen, Boxes, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { knowledgeBasesQueryOptions } from './queries'

export function KnowledgeBaseList() {
  const { t } = useTranslation()
  const { workspaceSlug } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/kb/',
  })
  const { data: knowledgeBases = [] } = useQuery(
    knowledgeBasesQueryOptions(workspaceSlug)
  )

  return (
    <div className='space-y-6'>
      <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
        <div>
          <p className='page-eyebrow'>{t('knowledgeBases.list.eyebrow')}</p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('knowledgeBases.list.title')}
          </h1>
          <p className='mt-2 max-w-2xl text-muted-foreground'>
            {t('knowledgeBases.list.subtitle')}
          </p>
        </div>
        <Button asChild>
          <Link
            to='/workspaces/$workspaceSlug/kb/new'
            params={{ workspaceSlug }}
          >
            <Plus />
            {t('knowledgeBases.list.createButton')}
          </Link>
        </Button>
      </div>

      {knowledgeBases.length === 0 ? (
        <Card className='border-dashed'>
          <CardContent className='flex min-h-48 flex-col items-center justify-center text-center'>
            <div className='mb-4 flex size-11 items-center justify-center rounded-lg bg-muted'>
              <BookOpen className='size-5 text-muted-foreground' />
            </div>
            <h2 className='font-medium'>
              {t('knowledgeBases.list.emptyTitle')}
            </h2>
            <p className='mt-2 max-w-md text-muted-foreground text-sm'>
              {t('knowledgeBases.list.emptyDescription')}
            </p>
            <Button asChild className='mt-5'>
              <Link
                to='/workspaces/$workspaceSlug/kb/new'
                params={{ workspaceSlug }}
              >
                <Plus />
                {t('knowledgeBases.list.createFirstButton')}
              </Link>
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
          {knowledgeBases.map((knowledgeBase) => (
            <Card key={knowledgeBase.id} className='group resource-card'>
              <CardHeader>
                <div className='icon-tile mb-3'>
                  <BookOpen className='size-5' />
                </div>
                <CardTitle>{knowledgeBase.name}</CardTitle>
                <CardDescription className='line-clamp-2 min-h-10'>
                  {knowledgeBase.description ||
                    t('knowledgeBases.list.noDescription')}
                </CardDescription>
                <CardAction>
                  <Button variant='ghost' size='icon' asChild>
                    <Link
                      to='/workspaces/$workspaceSlug/kb/$kbId'
                      params={{ workspaceSlug, kbId: knowledgeBase.id }}
                      aria-label={t('knowledgeBases.list.viewAriaLabel', {
                        name: knowledgeBase.name,
                      })}
                    >
                      <ArrowRight className='transition-transform group-hover:translate-x-0.5' />
                    </Link>
                  </Button>
                </CardAction>
              </CardHeader>
              <CardContent className='space-y-3'>
                <div className='flex items-center gap-2 text-sm'>
                  <Boxes className='size-4 text-primary' />
                  <span className='min-w-0 truncate font-medium'>
                    {knowledgeBase.embedding_model.display_name}
                  </span>
                  {!knowledgeBase.embedding_model.available && (
                    <Badge variant='secondary'>
                      {t('knowledgeBases.list.modelUnavailable')}
                    </Badge>
                  )}
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t('knowledgeBases.list.embeddingMeta', {
                    provider:
                      knowledgeBase.embedding_model.provider_display_name,
                    dimensions: knowledgeBase.embedding_model.dimensions,
                    chunkSize: knowledgeBase.chunking_config.chunk_size,
                    chunkOverlap: knowledgeBase.chunking_config.chunk_overlap,
                  })}
                </p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
