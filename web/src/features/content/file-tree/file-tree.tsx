import {
  ChevronDown,
  ChevronRight,
  Folder,
  FolderOpen,
  FolderPlus,
  MoreHorizontal,
  Move,
  Pencil,
  Trash2,
} from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import {
  CreateFolderDialog,
  DeleteDialog,
  fileTreeActionErrorMessage,
  MoveDialog,
  RenameDialog,
} from './file-tree-actions'
import {
  filterTree,
  findNode,
  folderIds,
  visibleNodes,
} from './file-tree-model'
import type { FileTreeData, FileTreeNode } from './schemas'

type FileTreeProps = {
  tree: FileTreeData
  canManage?: boolean
  selectedFolderId?: string
  onSelectFolder: (node: FileTreeNode) => void
  onRenameNode?: (node: FileTreeNode, name: string) => Promise<void> | void
  onCreateFolder?: (parent: FileTreeNode, name: string) => Promise<void> | void
  onMoveNode?: (
    node: FileTreeNode,
    target: FileTreeNode
  ) => Promise<void> | void
  onDeleteNode?: (node: FileTreeNode) => Promise<void> | void
}

export function FileTree({
  tree,
  canManage = false,
  selectedFolderId: controlledSelectedFolderId,
  onSelectFolder,
  onRenameNode,
  onCreateFolder,
  onMoveNode,
  onDeleteNode,
}: FileTreeProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(folderIds(tree.root))
  )
  const [focusedId, setFocusedId] = useState(
    tree.root.children[0]?.id ?? tree.root.id
  )
  const [renamingNode, setRenamingNode] = useState<FileTreeNode>()
  const [uncontrolledSelectedFolderId, setUncontrolledSelectedFolderId] =
    useState<string | undefined>(tree.root.id)
  const [creating, setCreating] = useState(false)
  const [movingNode, setMovingNode] = useState<FileTreeNode>()
  const [deletingNode, setDeletingNode] = useState<FileTreeNode>()
  const [actionError, setActionError] = useState<string>()
  const [query, setQuery] = useState('')
  const itemRefs = useRef(new Map<string, HTMLButtonElement>())
  const filteredRoot = useMemo(
    () => filterTree(tree.root, query.trim().toLocaleLowerCase()),
    [query, tree.root]
  )
  const nodes = useMemo(
    () => (filteredRoot ? visibleNodes(filteredRoot, expanded) : []),
    [expanded, filteredRoot]
  )

  function focusNode(nodeId: string) {
    setFocusedId(nodeId)
    queueMicrotask(() => itemRefs.current.get(nodeId)?.focus())
  }

  function toggle(node: FileTreeNode, force?: boolean) {
    if (node.node_type !== 'folder' && node.node_type !== 'root') return
    setExpanded((current) => {
      const next = new Set(current)
      const shouldExpand = force ?? !next.has(node.id)
      if (shouldExpand) next.add(node.id)
      else next.delete(node.id)
      return next
    })
  }

  function selectNode(node: FileTreeNode) {
    setActionError(undefined)
    setCreating(false)
    setUncontrolledSelectedFolderId(node.id)
    onSelectFolder(node)
  }

  function handleKeyDown(
    event: React.KeyboardEvent<HTMLButtonElement>,
    node: FileTreeNode,
    index: number
  ) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const delta = event.key === 'ArrowDown' ? 1 : -1
      const target =
        nodes[Math.max(0, Math.min(nodes.length - 1, index + delta))]
      if (target) focusNode(target.node.id)
      return
    }
    if (event.key === 'ArrowRight') {
      event.preventDefault()
      if (
        (node.node_type === 'folder' || node.node_type === 'root') &&
        !expanded.has(node.id)
      ) {
        toggle(node, true)
        return
      }
      const firstChild = node.children[0]
      if (firstChild) focusNode(firstChild.id)
      return
    }
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      if (
        (node.node_type === 'folder' || node.node_type === 'root') &&
        expanded.has(node.id)
      ) {
        toggle(node, false)
        return
      }
      if (node.parent_id && node.parent_id !== tree.root.id) {
        focusNode(node.parent_id)
      }
      return
    }
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      selectNode(node)
      return
    }
    if (
      event.key === 'F2' &&
      node.node_type === 'folder' &&
      canManage &&
      onRenameNode
    ) {
      event.preventDefault()
      setRenamingNode(node)
    }
  }

  const activeFolder = useMemo(() => {
    const targetId =
      controlledSelectedFolderId ?? uncontrolledSelectedFolderId ?? tree.root.id
    const found = findNode(tree.root, targetId)
    if (found && (found.node_type === 'root' || found.node_type === 'folder')) {
      return found
    }
    return tree.root
  }, [controlledSelectedFolderId, tree.root, uncontrolledSelectedFolderId])

  return (
    <div className='flex min-w-0 flex-col'>
      <div className='mb-3 flex items-center justify-between gap-2 px-2'>
        <div className='flex min-w-0 items-center gap-2'>
          <FolderOpen className='size-4 shrink-0 text-primary' />
          <h2 className='truncate font-medium text-sm'>{tree.root.name}</h2>
        </div>
        {canManage && onCreateFolder && (
          <Button
            type='button'
            variant='ghost'
            size='icon'
            aria-label={t('content.fileTree.newFolderAriaLabel')}
            onClick={() => {
              setCreating(true)
            }}
          >
            <FolderPlus />
          </Button>
        )}
      </div>
      <div className='mb-3 px-2'>
        <Input
          value={query}
          aria-label={t('content.fileTree.searchAriaLabel')}
          placeholder={t('content.fileTree.searchPlaceholder')}
          className='h-8'
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      {actionError && (
        <p
          className='mx-2 mb-3 rounded-md bg-destructive/10 p-2 text-destructive text-xs'
          role='alert'
        >
          {actionError}
        </p>
      )}
      {nodes.length === 0 ? (
        <p className='rounded-lg border border-dashed p-5 text-center text-muted-foreground text-sm'>
          {query.trim()
            ? t('content.fileTree.noMatches')
            : t('content.fileTree.empty')}
        </p>
      ) : (
        <div
          role='tree'
          aria-label={t('content.fileTree.treeAriaLabel')}
          className='space-y-0.5'
        >
          {nodes.map(({ node, level }, index) => {
            const isFolder = node.node_type === 'folder'
            const isRoot = node.node_type === 'root'
            const isExpandable = isFolder || isRoot
            const isExpanded = isExpandable && expanded.has(node.id)
            const isSelectedFolder = node.id === activeFolder.id
            return (
              <div
                key={node.id}
                className='group flex min-w-0 items-center'
                style={{
                  paddingInlineStart: `${Math.max(0, level - 1) * 16}px`,
                }}
              >
                {isExpandable ? (
                  <button
                    type='button'
                    aria-label={isExpanded ? '收起' : '展开'}
                    className='flex size-5 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground'
                    onClick={(event) => {
                      event.stopPropagation()
                      toggle(node)
                    }}
                  >
                    {isExpanded ? (
                      <ChevronDown className='size-3.5' />
                    ) : (
                      <ChevronRight className='size-3.5' />
                    )}
                  </button>
                ) : (
                  <span className='size-5 shrink-0' />
                )}
                <button
                  ref={(element) => {
                    if (element) itemRefs.current.set(node.id, element)
                    else itemRefs.current.delete(node.id)
                  }}
                  type='button'
                  role='treeitem'
                  aria-label={node.name}
                  aria-level={level}
                  aria-selected={isSelectedFolder}
                  aria-expanded={isExpandable ? isExpanded : undefined}
                  tabIndex={focusedId === node.id ? 0 : -1}
                  title={node.name}
                  className={cn(
                    'flex h-9 min-w-0 flex-1 items-center gap-1.5 rounded-md px-2 text-left text-sm outline-none transition-colors',
                    'hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50',
                    isSelectedFolder && 'bg-primary/10 text-primary'
                  )}
                  onClick={() => {
                    setFocusedId(node.id)
                    selectNode(node)
                  }}
                  onKeyDown={(event) => handleKeyDown(event, node, index)}
                >
                  <Folder className='size-4 shrink-0 text-primary' />
                  <span className='truncate'>{node.name}</span>
                </button>
                {canManage &&
                  isFolder &&
                  (onRenameNode || onMoveNode || onDeleteNode) && (
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='size-7 shrink-0 opacity-0 focus-visible:opacity-100 group-hover:opacity-100 data-[state=open]:opacity-100'
                          aria-label={t(
                            'content.fileTree.rowActionsAriaLabel',
                            {
                              name: node.name,
                            }
                          )}
                        >
                          <MoreHorizontal className='size-4' />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align='end'>
                        {onRenameNode && (
                          <DropdownMenuItem
                            onClick={() => setRenamingNode(node)}
                          >
                            <Pencil className='size-4' />
                            {t('content.fileTree.renameAction')}
                          </DropdownMenuItem>
                        )}
                        {onMoveNode && (
                          <DropdownMenuItem onClick={() => setMovingNode(node)}>
                            <Move className='size-4' />
                            {t('content.fileTree.moveAction')}
                          </DropdownMenuItem>
                        )}
                        {onDeleteNode && (
                          <>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              variant='destructive'
                              onClick={() => setDeletingNode(node)}
                            >
                              <Trash2 className='size-4' />
                              {t('content.fileTree.deleteAction')}
                            </DropdownMenuItem>
                          </>
                        )}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  )}
              </div>
            )
          })}
        </div>
      )}
      {movingNode && onMoveNode && (
        <MoveDialog
          node={movingNode}
          root={tree.root}
          open={Boolean(movingNode)}
          onOpenChange={(open) => {
            if (!open) setMovingNode(undefined)
          }}
          onMove={onMoveNode}
          onError={setActionError}
        />
      )}
      {deletingNode && onDeleteNode && (
        <DeleteDialog
          node={deletingNode}
          open={Boolean(deletingNode)}
          onOpenChange={(open) => {
            if (!open) setDeletingNode(undefined)
          }}
          onDelete={onDeleteNode}
          onDeleted={() => {
            setDeletingNode(undefined)
            setUncontrolledSelectedFolderId(tree.root.id)
          }}
          onError={setActionError}
        />
      )}
      {creating && onCreateFolder && (
        <CreateFolderDialog
          parent={activeFolder}
          open={creating}
          onOpenChange={setCreating}
          onCreate={async (name) => {
            try {
              await onCreateFolder(activeFolder, name)
            } catch (error) {
              setActionError(fileTreeActionErrorMessage(error))
              throw error
            }
          }}
        />
      )}
      {renamingNode && onRenameNode && (
        <RenameDialog
          node={renamingNode}
          open={Boolean(renamingNode)}
          onOpenChange={(open) => {
            if (!open) setRenamingNode(undefined)
          }}
          onError={setActionError}
          onSave={async (name) => {
            await onRenameNode(renamingNode, name)
            focusNode(renamingNode.id)
          }}
        />
      )}
    </div>
  )
}
