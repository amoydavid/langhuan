import { Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  ArrowRight,
  BookOpen,
  CheckCircle2,
  Circle,
  DatabaseZap,
  FileText,
  Search,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import type { WorkspaceReadiness } from './types'

type RecentKnowledgeBase = {
  id: string
  name: string
  description: string
}

type WorkspaceReadinessPanelProps = {
  workspaceSlug: string
  readiness: WorkspaceReadiness
  knowledgeBases: RecentKnowledgeBase[]
  canManageWorkspace: boolean
  canManageInvitations: boolean
}

type NextAction = {
  eyebrow: string
  title: string
  description: string
  label?: string
  href?: string
}

function workspacePath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}`
}

function knowledgeBasePath(workspaceSlug: string, kbId: string) {
  return `${workspacePath(workspaceSlug)}/kb/${encodeURIComponent(kbId)}`
}

function ReadinessRow({
  complete,
  warning = false,
  label,
  detail,
}: {
  complete: boolean
  warning?: boolean
  label: string
  detail: string
}) {
  const Icon = warning ? AlertTriangle : complete ? CheckCircle2 : Circle
  return (
    <li className='flex items-start gap-3 py-3 first:pt-0 last:pb-0'>
      <Icon
        aria-hidden='true'
        className={
          warning
            ? 'mt-0.5 size-4 shrink-0 text-destructive'
            : complete
              ? 'mt-0.5 size-4 shrink-0 text-primary'
              : 'mt-0.5 size-4 shrink-0 text-muted-foreground'
        }
      />
      <div className='min-w-0 flex-1 sm:flex sm:items-baseline sm:justify-between sm:gap-4'>
        <p className='font-medium text-sm'>{label}</p>
        <p className='mt-0.5 text-muted-foreground text-sm sm:mt-0 sm:text-right'>
          {detail}
        </p>
      </div>
    </li>
  )
}

export function WorkspaceReadinessPanel({
  workspaceSlug,
  readiness,
  knowledgeBases,
  canManageWorkspace,
  canManageInvitations,
}: WorkspaceReadinessPanelProps) {
  const { t } = useTranslation()
  const counts = readiness.document_counts

  function nextAction(): NextAction {
    const base = workspacePath(workspaceSlug)
    const kbName =
      readiness.recommended_knowledge_base_name ||
      t('workspaceReadiness.recentKnowledgeBase')
    const documentName =
      readiness.recommended_document_name ||
      t('workspaceReadiness.relatedContent')
    const kbId = readiness.recommended_knowledge_base_id
    const documentId = readiness.recommended_document_id

    switch (readiness.recommended_action) {
      case 'configure_provider':
        return canManageWorkspace
          ? {
              eyebrow: t('workspaceReadiness.nextAction.provider.eyebrow'),
              title: t('workspaceReadiness.nextAction.provider.title'),
              description: t(
                'workspaceReadiness.nextAction.provider.description'
              ),
              label: t('workspaceReadiness.nextAction.provider.label'),
              href: `${base}/models`,
            }
          : {
              eyebrow: t('workspaceReadiness.nextAction.waitingAdmin'),
              title: t('workspaceReadiness.nextAction.provider.memberTitle'),
              description: t(
                'workspaceReadiness.nextAction.provider.memberDescription'
              ),
            }
      case 'create_embedding_model':
        return canManageWorkspace
          ? {
              eyebrow: t(
                'workspaceReadiness.nextAction.embeddingModel.eyebrow'
              ),
              title: t('workspaceReadiness.nextAction.embeddingModel.title'),
              description: t(
                'workspaceReadiness.nextAction.embeddingModel.description'
              ),
              label: t('workspaceReadiness.nextAction.embeddingModel.label'),
              href: `${base}/models`,
            }
          : {
              eyebrow: t('workspaceReadiness.nextAction.waitingAdmin'),
              title: t(
                'workspaceReadiness.nextAction.embeddingModel.memberTitle'
              ),
              description: t(
                'workspaceReadiness.nextAction.embeddingModel.memberDescription'
              ),
            }
      case 'create_knowledge_base':
        return {
          eyebrow: t('workspaceReadiness.nextAction.knowledgeBase.eyebrow'),
          title: t('workspaceReadiness.nextAction.knowledgeBase.title'),
          description: t(
            'workspaceReadiness.nextAction.knowledgeBase.description'
          ),
          label: t('workspaceReadiness.nextAction.knowledgeBase.label'),
          href: `${base}/kb/new`,
        }
      case 'add_content':
        return {
          eyebrow: t('workspaceReadiness.nextAction.addContent.eyebrow', {
            kbName,
          }),
          title: t('workspaceReadiness.nextAction.addContent.title', {
            kbName,
          }),
          description: t(
            'workspaceReadiness.nextAction.addContent.description'
          ),
          label: t('workspaceReadiness.nextAction.addContent.label'),
          href: kbId
            ? `${knowledgeBasePath(workspaceSlug, kbId)}/documents/new`
            : `${base}/kb`,
        }
      case 'wait_for_processing':
        return {
          eyebrow: t('workspaceReadiness.nextAction.waiting.eyebrow', {
            kbName,
          }),
          title: t('workspaceReadiness.nextAction.waiting.title', {
            documentName,
          }),
          description: t('workspaceReadiness.nextAction.waiting.description'),
          label: t('workspaceReadiness.nextAction.waiting.label'),
          href: documentId
            ? `${base}/documents/${encodeURIComponent(documentId)}`
            : kbId
              ? knowledgeBasePath(workspaceSlug, kbId)
              : `${base}/kb`,
        }
      case 'resolve_failed_document':
        return {
          eyebrow: t('workspaceReadiness.nextAction.failed.eyebrow', {
            kbName,
          }),
          title: documentName,
          description: t('workspaceReadiness.nextAction.failed.description'),
          label: t('workspaceReadiness.nextAction.failed.label'),
          href: documentId
            ? `${base}/documents/${encodeURIComponent(documentId)}`
            : kbId
              ? knowledgeBasePath(workspaceSlug, kbId)
              : `${base}/kb`,
        }
      case 'test_retrieval':
        return {
          eyebrow: t('workspaceReadiness.nextAction.testRetrieval.eyebrow', {
            kbName,
          }),
          title: t('workspaceReadiness.nextAction.testRetrieval.title', {
            kbName,
          }),
          description: t(
            'workspaceReadiness.nextAction.testRetrieval.description'
          ),
          label: t('workspaceReadiness.nextAction.testRetrieval.label'),
          href: kbId
            ? `${knowledgeBasePath(workspaceSlug, kbId)}/search`
            : `${base}/kb`,
        }
      case 'none':
        return {
          eyebrow: t('workspaceReadiness.nextAction.complete.eyebrow'),
          title: t('workspaceReadiness.nextAction.complete.title'),
          description: t('workspaceReadiness.nextAction.complete.description'),
        }
    }
  }

  const action = nextAction()

  return (
    <div className='space-y-5'>
      <Card className='overflow-hidden border-primary/25 bg-primary/[0.025]'>
        <CardContent className='flex flex-col gap-5 p-5 sm:flex-row sm:items-center sm:justify-between sm:p-6'>
          <div className='min-w-0'>
            <p className='font-medium text-primary text-xs uppercase tracking-[0.16em]'>
              {action.eyebrow}
            </p>
            <h2 className='mt-2 font-semibold text-xl tracking-tight'>
              {action.title}
            </h2>
            <p className='mt-2 max-w-2xl text-muted-foreground text-sm leading-6'>
              {action.description}
            </p>
          </div>
          {action.href && action.label && (
            <Button asChild className='shrink-0'>
              <a href={action.href}>
                {action.label}
                <ArrowRight />
              </a>
            </Button>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <DatabaseZap className='size-4 text-primary' />
            {t('workspaceReadiness.panel.title')}
          </CardTitle>
          <CardDescription>
            {t('workspaceReadiness.panel.description')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ul className='divide-y divide-border'>
            <ReadinessRow
              complete={readiness.has_active_provider}
              label={t('workspaceReadiness.rows.provider.label')}
              detail={
                readiness.has_active_provider
                  ? t('workspaceReadiness.rows.provider.configured')
                  : t('workspaceReadiness.rows.provider.notConfigured')
              }
            />
            <ReadinessRow
              complete={readiness.has_selectable_embedding_model}
              label={t('workspaceReadiness.rows.embeddingModel.label')}
              detail={
                readiness.has_selectable_embedding_model
                  ? t('workspaceReadiness.rows.embeddingModel.available')
                  : t('workspaceReadiness.rows.embeddingModel.unavailable')
              }
            />
            <ReadinessRow
              complete={readiness.knowledge_base_count > 0}
              label={t('workspaceReadiness.rows.knowledgeBase.label')}
              detail={t('workspaceReadiness.rows.knowledgeBase.count', {
                count: readiness.knowledge_base_count,
              })}
            />
            <ReadinessRow
              complete={counts.total > 0 && counts.failed === 0}
              warning={counts.failed > 0}
              label={t('workspaceReadiness.rows.content.label')}
              detail={t('workspaceReadiness.rows.content.detail', {
                ready: counts.ready,
                processing: counts.processing,
                failed: counts.failed,
              })}
            />
            <ReadinessRow
              complete={readiness.searchable_knowledge_base_count > 0}
              label={t('workspaceReadiness.rows.retrieval.label')}
              detail={
                readiness.searchable_knowledge_base_count > 0
                  ? t('workspaceReadiness.rows.retrieval.ready', {
                      count: readiness.searchable_knowledge_base_count,
                    })
                  : t('workspaceReadiness.rows.retrieval.waiting')
              }
            />
          </ul>
        </CardContent>
      </Card>

      <div className='grid gap-5 lg:grid-cols-[minmax(0,1.3fr)_minmax(18rem,0.7fr)]'>
        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              <BookOpen className='size-4 text-primary' />
              {t('workspaceReadiness.recent.title')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {knowledgeBases.length === 0 ? (
              <div className='rounded-lg border border-dashed p-6 text-center'>
                <p className='font-medium text-sm'>
                  {t('workspaceReadiness.recent.emptyTitle')}
                </p>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t('workspaceReadiness.recent.emptyDescription')}
                </p>
              </div>
            ) : (
              <ul className='divide-y divide-border'>
                {knowledgeBases.slice(0, 3).map((knowledgeBase) => (
                  <li key={knowledgeBase.id}>
                    <Link
                      to='/workspaces/$workspaceSlug/kb/$kbId'
                      params={{ workspaceSlug, kbId: knowledgeBase.id }}
                      className='group flex min-h-16 items-center justify-between gap-4 py-3'
                    >
                      <div className='min-w-0'>
                        <p className='truncate font-medium text-sm'>
                          {knowledgeBase.name ||
                            t('workspaceReadiness.recent.unnamed')}
                        </p>
                        <p className='mt-1 truncate text-muted-foreground text-xs'>
                          {knowledgeBase.description ||
                            t('workspaceReadiness.recent.noDescription')}
                        </p>
                      </div>
                      <ArrowRight className='size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5' />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className='text-base'>
              {t('workspaceReadiness.quickLinks.title')}
            </CardTitle>
          </CardHeader>
          <CardContent className='grid gap-2'>
            <Button variant='outline' asChild className='justify-start'>
              <Link
                to='/workspaces/$workspaceSlug/kb/new'
                params={{ workspaceSlug }}
              >
                <BookOpen />
                {t('workspaceReadiness.quickLinks.newKnowledgeBase')}
              </Link>
            </Button>
            <Button variant='outline' asChild className='justify-start'>
              <Link
                to='/workspaces/$workspaceSlug/models'
                params={{ workspaceSlug }}
              >
                <DatabaseZap />
                {t('workspaceReadiness.quickLinks.models')}
              </Link>
            </Button>
            {canManageInvitations ? (
              <Button variant='outline' asChild className='justify-start'>
                <Link
                  to='/workspaces/$workspaceSlug/invitations'
                  params={{ workspaceSlug }}
                >
                  <FileText />
                  {t('workspaceReadiness.quickLinks.membersAndInvitations')}
                </Link>
              </Button>
            ) : (
              <Button variant='outline' asChild className='justify-start'>
                <Link
                  to='/workspaces/$workspaceSlug/members'
                  params={{ workspaceSlug }}
                >
                  <FileText />
                  {t('workspaceReadiness.quickLinks.members')}
                </Link>
              </Button>
            )}
            {readiness.recommended_knowledge_base_id && (
              <Button variant='outline' asChild className='justify-start'>
                <a
                  href={`${knowledgeBasePath(workspaceSlug, readiness.recommended_knowledge_base_id)}/search`}
                >
                  <Search />
                  {t('workspaceReadiness.quickLinks.searchTest')}
                </a>
              </Button>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
