import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Outlet, useNavigate, useParams } from '@tanstack/react-router'
import { Upload } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { meQueryOptions } from '@/features/auth/queries'
import { deleteDocument } from '@/features/documents/api'
import { canManageContent } from '@/features/knowledge-bases/permissions'
import {
  createFileTreeFolder,
  deleteFileTreeNode,
  updateFileTreeNode,
} from './api'
import { FileTree } from './file-tree'
import { fileTreeQueryOptions } from './queries'
import type { FileTreeNode } from './schemas'

type TreeMutation =
  | { type: 'create'; parent: FileTreeNode; name: string }
  | { type: 'rename'; node: FileTreeNode; name: string }
  | { type: 'move'; node: FileTreeNode; target: FileTreeNode }
  | { type: 'delete'; node: FileTreeNode }

export function FileTreeWorkspace() {
  const { t } = useTranslation()
  const params = useParams({ strict: false }) as {
    workspaceSlug: string
    kbId: string
    documentId?: string
  }
  const { workspaceSlug, kbId, documentId } = params
  const { data: tree } = useQuery(fileTreeQueryOptions(workspaceSlug, kbId))
  const { data: me } = useQuery(meQueryOptions())
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [selectedFolderId, setSelectedFolderId] = useState<string>()
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  const canManage = canManageContent(role)

  const mutation = useMutation({
    mutationFn: async (input: TreeMutation) => {
      if (input.type === 'create') {
        return createFileTreeFolder(workspaceSlug, kbId, {
          parent_id: input.parent.id,
          name: input.name,
        })
      }
      if (input.type === 'rename') {
        await updateFileTreeNode(workspaceSlug, kbId, input.node.id, {
          name: input.name,
        })
        return
      }
      if (input.type === 'move') {
        await updateFileTreeNode(workspaceSlug, kbId, input.node.id, {
          parent_id: input.target.id,
        })
        return
      }
      if (input.node.node_type === 'file' && input.node.document_id) {
        await deleteDocument(workspaceSlug, input.node.document_id)
        return
      }
      await deleteFileTreeNode(workspaceSlug, kbId, input.node.id)
    },
    onSuccess: async (_result, input) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['file-tree', workspaceSlug, kbId],
        }),
        queryClient.invalidateQueries({
          queryKey: ['documents', workspaceSlug, kbId],
        }),
        queryClient.invalidateQueries({
          queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
        }),
      ])
      if (
        input.type === 'delete' &&
        input.node.node_type === 'file' &&
        input.node.document_id === documentId
      ) {
        await navigate({
          to: '/workspaces/$workspaceSlug/kb/$kbId/content/files',
          params: { workspaceSlug, kbId },
          replace: true,
        })
      }
    },
  })

  if (!tree) return null
  const uploadParentId = selectedFolderId ?? tree.root.id
  return (
    <div className='grid min-h-[36rem] overflow-hidden rounded-xl border bg-card xl:grid-cols-[minmax(15rem,19rem)_minmax(0,1fr)]'>
      <aside className='min-w-0 border-b bg-muted/15 p-3 xl:border-r xl:border-b-0'>
        <div className='mb-3 flex justify-end'>
          {canManage && (
            <Button asChild size='sm'>
              <a
                href={`/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/files/upload?parent=${encodeURIComponent(uploadParentId)}`}
              >
                <Upload />
                {t('content.fileWorkspace.uploadFile')}
              </a>
            </Button>
          )}
        </div>
        <FileTree
          tree={tree}
          selectedDocumentId={documentId}
          canManage={canManage}
          onSelectDocument={(nextDocumentId) =>
            void navigate({
              to: '/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId',
              params: { workspaceSlug, kbId, documentId: nextDocumentId },
              search: {},
            })
          }
          onSelectFolder={(folder) => setSelectedFolderId(folder.id)}
          onCreateFolder={async (parent, name) => {
            await mutation.mutateAsync({ type: 'create', parent, name })
          }}
          onRenameNode={async (node, name) => {
            await mutation.mutateAsync({ type: 'rename', node, name })
          }}
          onMoveNode={async (node, target) => {
            await mutation.mutateAsync({ type: 'move', node, target })
          }}
          onDeleteNode={async (node) => {
            await mutation.mutateAsync({ type: 'delete', node })
          }}
        />
      </aside>
      <div className='min-w-0 p-4 lg:p-5'>
        <Outlet />
      </div>
    </div>
  )
}
