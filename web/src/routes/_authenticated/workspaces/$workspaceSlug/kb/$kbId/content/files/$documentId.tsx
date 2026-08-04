import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { Workflow } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { meQueryOptions } from '@/features/auth/queries'
import { createChunkRevision } from '@/features/chunks/api'
import { ChunkDetailDialog } from '@/features/chunks/inspector/chunk-detail-dialog'
import { ChunkInspector } from '@/features/chunks/inspector/chunk-inspector'
import {
  chunkRevisionsQueryOptions,
  documentChunksQueryOptions,
} from '@/features/chunks/queries'
import { ChunkRevisionForm } from '@/features/chunks/revision-editor/chunk-revision-form'
import type {
  Chunk,
  ChunkRevision,
  CreateChunkRevisionInput,
} from '@/features/chunks/types'
import { DocumentPreview } from '@/features/content/document-preview/document-preview'
import { fileTreeQueryOptions } from '@/features/content/file-tree/queries'
import { canonicalDocumentHref, findFileNode } from '@/features/content/routing'
import { documentQueryOptions } from '@/features/documents/queries'
import { canManageIndex } from '@/features/knowledge-bases/permissions'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'
import { useMediaQuery } from '@/hooks/use-media-query'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'

const fileDetailSearchSchema = z.object({
  chunk: z.string().optional(),
  anchor: z.number().int().nonnegative().optional(),
  enabled: z.boolean().optional(),
  job: z.string().optional(),
  page: z.number().int().positive().optional(),
})

type FileDetailLoaderData = { documentName: string }

function loaderDocumentName(data: unknown) {
  if (
    typeof data === 'object' &&
    data !== null &&
    'documentName' in data &&
    typeof data.documentName === 'string'
  ) {
    return data.documentName
  }
  return undefined
}

function FileDetailPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId, documentId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const queryClient = useQueryClient()
  const { data: item } = useQuery(
    documentQueryOptions(workspaceSlug, documentId)
  )
  const { data: tree } = useQuery(fileTreeQueryOptions(workspaceSlug, kbId))
  const { data: me } = useQuery(meQueryOptions())
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  const activeGenerationId =
    item?.status === 'ready' ? (summary?.active_generation?.id ?? '') : ''
  const chunkOptions = documentChunksQueryOptions(
    workspaceSlug,
    kbId,
    documentId,
    activeGenerationId,
    search.enabled ? { enabled: true } : {}
  )
  const { data: chunkPage } = useQuery(chunkOptions)
  const chunks = chunkPage?.items ?? []
  const selectedChunkId = search.chunk
  const selectedChunk = chunks.find((item) => item.id === selectedChunkId)
  const page = search.page ?? 1
  const revisionOptions = chunkRevisionsQueryOptions(
    workspaceSlug,
    kbId,
    selectedChunkId ?? ''
  )
  const { data: revisions = [] } = useQuery({
    ...revisionOptions,
    enabled: Boolean(selectedChunkId),
  })
  const [editing, setEditing] = useState<Chunk>()
  const [latestRevision, setLatestRevision] = useState<ChunkRevision>()
  const [editorDirty, setEditorDirty] = useState(false)
  const [chunkPanelOpen, setChunkPanelOpen] = useState(Boolean(search.chunk))
  const wideDesktop = useMediaQuery('(min-width: 1280px)')

  useEffect(() => {
    if (search.chunk && !wideDesktop) setChunkPanelOpen(true)
  }, [search.chunk, wideDesktop])

  if (!item || !tree || !summary) return null
  const node = findFileNode(tree.root, documentId)
  const displayName =
    node?.name ||
    item.title ||
    t('routes.workspaces.kb.content.files.detail.unnamedTitle')
  const path = (node?.path || displayName).replace(/^\//, '')
  const role = me?.workspaces.find(
    (membership) => membership.slug === workspaceSlug
  )?.role
  const canEdit = canManageIndex(role)

  async function saveRevision(input: CreateChunkRevisionInput) {
    if (!editing) throw new Error('缺少待编辑分块')
    try {
      return await createChunkRevision(workspaceSlug, kbId, editing.id, input)
    } catch (error) {
      if (parseApiError(error).code === 'revision_conflict') {
        const latest = await queryClient.fetchQuery(
          chunkRevisionsQueryOptions(workspaceSlug, kbId, editing.id)
        )
        setLatestRevision(latest[0])
      }
      throw error
    }
  }

  async function revisionSaved() {
    if (!editing) return
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: chunkOptions.queryKey }),
      queryClient.invalidateQueries({
        queryKey: ['chunk', workspaceSlug, kbId, editing.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ['chunk-revisions', workspaceSlug, kbId, editing.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
      }),
    ])
    setEditing(undefined)
    setLatestRevision(undefined)
    setEditorDirty(false)
  }

  function closeEditor() {
    if (
      editorDirty &&
      !window.confirm(
        t('routes.workspaces.kb.content.files.detail.confirmLeave')
      )
    ) {
      return
    }
    setEditing(undefined)
    setLatestRevision(undefined)
    setEditorDirty(false)
  }

  const inspector = (
    <ChunkInspector
      documentTitle={displayName}
      documentKind='file'
      chunks={chunks}
      selectedChunkId={selectedChunkId}
      page={page}
      canEdit={canEdit}
      onSelectChunk={(chunkId) =>
        void navigate({
          search: { ...search, chunk: chunkId, page: undefined },
        })
      }
      onPageChange={(next) =>
        void navigate({
          search: { ...search, page: next },
          replace: true,
        })
      }
      onEdit={(chunk) => {
        setLatestRevision(undefined)
        setEditorDirty(false)
        setEditing(chunk)
      }}
    />
  )

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <label className='flex items-center gap-2 text-sm'>
          <input
            type='checkbox'
            checked={search.enabled === true}
            onChange={(event) =>
              void navigate({
                search: {
                  ...search,
                  enabled: event.target.checked || undefined,
                },
                replace: true,
              })
            }
          />
          {t('routes.workspaces.kb.content.files.detail.showOnlySearchable')}
        </label>
        {search.job && (
          <Button variant='outline' size='sm' asChild>
            <a
              href={`/workspaces/${encodeURIComponent(workspaceSlug)}/jobs/${encodeURIComponent(search.job)}`}
            >
              <Workflow />
              {t('routes.workspaces.kb.content.files.detail.viewJob')}
            </a>
          </Button>
        )}
      </div>

      <div className='grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,25rem)]'>
        <DocumentPreview
          document={item}
          displayName={displayName}
          path={path}
          initialView={search.anchor !== undefined ? 'raw' : 'preview'}
        />
        {wideDesktop ? (
          inspector
        ) : (
          <Sheet open={chunkPanelOpen} onOpenChange={setChunkPanelOpen}>
            <SheetTrigger asChild>
              <Button variant='outline'>
                {t('routes.workspaces.kb.content.files.detail.viewChunks', {
                  count: chunks.length,
                })}
              </Button>
            </SheetTrigger>
            <SheetContent className='w-full overflow-y-auto sm:max-w-xl'>
              <SheetHeader>
                <SheetTitle>
                  {t('routes.workspaces.kb.content.files.detail.sheetTitle', {
                    name: displayName,
                  })}
                </SheetTitle>
                <SheetDescription>
                  {t(
                    'routes.workspaces.kb.content.files.detail.sheetDescription'
                  )}
                </SheetDescription>
              </SheetHeader>
              <div className='p-4 pt-0'>{inspector}</div>
            </SheetContent>
          </Sheet>
        )}
      </div>

      <Dialog
        open={Boolean(editing)}
        onOpenChange={(open) => {
          if (!open) closeEditor()
        }}
      >
        <DialogContent className='max-h-[90svh] overflow-y-auto sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>
              {t('routes.workspaces.kb.content.files.detail.dialogTitle')}
            </DialogTitle>
            <DialogDescription>
              {t('routes.workspaces.kb.content.files.detail.dialogDescription')}
            </DialogDescription>
          </DialogHeader>
          {editing && (
            <ChunkRevisionForm
              chunk={editing}
              latestRevision={latestRevision}
              saveRevision={saveRevision}
              onSaved={() => void revisionSaved()}
              onCancel={closeEditor}
              onDirtyChange={setEditorDirty}
            />
          )}
        </DialogContent>
      </Dialog>

      <ChunkDetailDialog
        chunk={selectedChunk}
        documentTitle={displayName}
        documentKind='file'
        revisions={selectedChunk ? revisions : []}
        canEdit={canEdit}
        onOpenChange={(open) => {
          if (!open) {
            void navigate({
              search: { ...search, chunk: undefined },
              replace: true,
            })
          }
        }}
        onEdit={(chunk) => {
          setLatestRevision(undefined)
          setEditorDirty(false)
          setEditing(chunk)
        }}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId'
)({
  validateSearch: fileDetailSearchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params, deps }): Promise<FileDetailLoaderData> => {
    const [item, tree, summary] = await Promise.all([
      context.queryClient.ensureQueryData(
        documentQueryOptions(params.workspaceSlug, params.documentId)
      ),
      context.queryClient.ensureQueryData(
        fileTreeQueryOptions(params.workspaceSlug, params.kbId)
      ),
      context.queryClient.ensureQueryData(
        knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
      ),
    ])
    if (item.kind !== 'file' || item.knowledge_base_id !== params.kbId) {
      throw redirect({
        href: canonicalDocumentHref(params.workspaceSlug, item),
      })
    }
    if (item.status === 'ready' && summary.active_generation?.id) {
      await context.queryClient.ensureQueryData(
        documentChunksQueryOptions(
          params.workspaceSlug,
          params.kbId,
          params.documentId,
          summary.active_generation.id,
          deps.enabled ? { enabled: true } : {}
        )
      )
    }
    return {
      documentName:
        findFileNode(tree.root, params.documentId)?.name ||
        item.title ||
        i18n.t('routes.workspaces.kb.content.files.detail.unnamedTitle'),
    }
  },
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.files.detail.breadcrumb',
      resolve: loaderDocumentName,
    },
  },
  component: FileDetailPage,
})
