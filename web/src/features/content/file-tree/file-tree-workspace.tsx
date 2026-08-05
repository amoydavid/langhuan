import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Outlet,
  useNavigate,
  useParams,
  useSearch,
} from '@tanstack/react-router'
import { useState } from 'react'
import { meQueryOptions } from '@/features/auth/queries'
import { deleteDocument } from '@/features/documents/api'
import { documentsQueryOptions } from '@/features/documents/queries'
import { canManageContent } from '@/features/knowledge-bases/permissions'
import {
  createFileTreeFolder,
  deleteFileTreeNode,
  updateFileTreeNode,
} from './api'
import { FileBrowserList } from './file-browser-list'
import { FileTree } from './file-tree'
import {
  CreateFolderDialog,
  DeleteDialog,
  fileTreeActionErrorMessage,
  MoveDialog,
  RenameDialog,
} from './file-tree-actions'
import { descendantIds, findNode } from './file-tree-model'
import { fileTreeQueryOptions } from './queries'
import type { FileTreeNode } from './schemas'
import { UploadFileDialog } from './upload-file-dialog'

type TreeMutation =
  | { type: 'create'; parent: FileTreeNode; name: string }
  | { type: 'rename'; node: FileTreeNode; name: string }
  | { type: 'move'; node: FileTreeNode; target: FileTreeNode }
  | { type: 'delete'; node: FileTreeNode }

export function FileTreeWorkspace() {
  const params = useParams({ strict: false }) as {
    workspaceSlug: string
    kbId: string
    documentId?: string
  }
  const { workspaceSlug, kbId, documentId } = params
  const search = useSearch({ strict: false }) as {
    folder?: string
    upload?: boolean
  }
  const { data: tree } = useQuery(fileTreeQueryOptions(workspaceSlug, kbId))
  const { data: documents = [] } = useQuery(
    documentsQueryOptions(workspaceSlug, kbId)
  )
  const { data: me } = useQuery(meQueryOptions())
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [uploadOpen, setUploadOpen] = useState(false)
  const [creatingFolder, setCreatingFolder] = useState(false)
  const [renamingNode, setRenamingNode] = useState<FileTreeNode>()
  const [movingNode, setMovingNode] = useState<FileTreeNode>()
  const [deletingNode, setDeletingNode] = useState<FileTreeNode>()
  const [actionError, setActionError] = useState<string>()
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
      if (
        input.type === 'delete' &&
        search.folder &&
        descendantIds(input.node).has(search.folder)
      ) {
        await navigate({
          to: '/workspaces/$workspaceSlug/kb/$kbId/content/files',
          params: { workspaceSlug, kbId },
          search: { upload: undefined, folder: undefined },
          replace: true,
        })
      }
    },
  })

  if (!tree) return null
  const selectedFolder =
    findNode(tree.root, search.folder ?? tree.root.id) ?? tree.root
  return (
    <div className='grid min-h-0 flex-1 overflow-hidden rounded-xl border bg-card md:grid-cols-[14rem_minmax(0,1fr)]'>
      <aside className='flex min-w-0 flex-col overflow-y-auto overflow-x-hidden border-b bg-muted/15 p-3 md:border-r md:border-b-0'>
        <FileTree
          tree={tree}
          canManage={canManage}
          selectedFolderId={selectedFolder.id}
          onSelectFolder={(folder) => {
            void navigate({
              to: '/workspaces/$workspaceSlug/kb/$kbId/content/files',
              params: { workspaceSlug, kbId },
              search: {
                folder: folder.id === tree.root.id ? undefined : folder.id,
                upload: search.upload,
              },
              replace: true,
            })
          }}
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
      <div className='flex min-w-0 flex-col overflow-y-auto overflow-x-hidden p-4 lg:p-5'>
        {actionError && (
          <p
            className='mb-3 rounded-md bg-destructive/10 p-2 text-destructive text-xs'
            role='alert'
          >
            {actionError}
          </p>
        )}
        <FileBrowserList
          folder={selectedFolder}
          documents={documents}
          canManage={canManage}
          onOpenFile={(nextDocumentId) =>
            void navigate({
              to: '/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId',
              params: { workspaceSlug, kbId, documentId: nextDocumentId },
              search: {
                folder:
                  selectedFolder.id === tree.root.id
                    ? undefined
                    : selectedFolder.id,
              },
            })
          }
          onUploadFile={() => setUploadOpen(true)}
          onCreateFolder={() => setCreatingFolder(true)}
          onRenameFile={setRenamingNode}
          onMoveFile={setMovingNode}
          onDeleteFile={setDeletingNode}
        />
        <Outlet />
      </div>
      {creatingFolder && (
        <CreateFolderDialog
          parent={selectedFolder}
          open={creatingFolder}
          onOpenChange={setCreatingFolder}
          onCreate={async (name) => {
            try {
              await mutation.mutateAsync({
                type: 'create',
                parent: selectedFolder,
                name,
              })
            } catch (error) {
              setActionError(fileTreeActionErrorMessage(error))
              throw error
            }
          }}
        />
      )}
      <UploadFileDialog
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        parentNodeId={selectedFolder.id}
        parentPath={selectedFolder.path}
        open={uploadOpen || search.upload === true}
        onOpenChange={(open) => {
          setUploadOpen(open)
          if (!open && search.upload) {
            void navigate({
              to: '/workspaces/$workspaceSlug/kb/$kbId/content/files',
              params: { workspaceSlug, kbId },
              search: { upload: undefined, folder: search.folder },
              replace: true,
            })
          }
        }}
      />
      {renamingNode && (
        <RenameDialog
          node={renamingNode}
          open={Boolean(renamingNode)}
          onOpenChange={(open) => {
            if (!open) setRenamingNode(undefined)
          }}
          onError={setActionError}
          onSave={async (name) => {
            await mutation.mutateAsync({
              type: 'rename',
              node: renamingNode,
              name,
            })
          }}
        />
      )}
      {movingNode && (
        <MoveDialog
          node={movingNode}
          root={tree.root}
          open={Boolean(movingNode)}
          onOpenChange={(open) => {
            if (!open) setMovingNode(undefined)
          }}
          onError={setActionError}
          onMove={async (node, target) => {
            await mutation.mutateAsync({ type: 'move', node, target })
          }}
        />
      )}
      {deletingNode && (
        <DeleteDialog
          node={deletingNode}
          open={Boolean(deletingNode)}
          onOpenChange={(open) => {
            if (!open) setDeletingNode(undefined)
          }}
          onError={setActionError}
          onDelete={async (node) => {
            await mutation.mutateAsync({ type: 'delete', node })
          }}
          onDeleted={() => setDeletingNode(undefined)}
        />
      )}
    </div>
  )
}
