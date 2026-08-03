import {
  ChevronDown,
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  Move,
  Pencil,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import {
  CreateFolderEditor,
  DeleteDialog,
  fileTreeActionErrorMessage,
  MoveDialog,
  RenameEditor,
} from './file-tree-actions'
import {
  allFolders,
  descendantIds,
  filterTree,
  findNode,
  folderIds,
  visibleNodes,
} from './file-tree-model'
import type { FileTreeData, FileTreeNode } from './schemas'

type FileTreeProps = {
  tree: FileTreeData
  selectedDocumentId?: string
  canManage?: boolean
  onSelectDocument: (documentId: string) => void
  onSelectFolder?: (node: FileTreeNode) => void
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
  selectedDocumentId,
  canManage = false,
  onSelectDocument,
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
  const [renamingId, setRenamingId] = useState<string>()
  const [activeNodeId, setActiveNodeId] = useState(tree.root.id)
  const [creating, setCreating] = useState(false)
  const [moving, setMoving] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [actionError, setActionError] = useState<string>()
  const [query, setQuery] = useState('')
  const itemRefs = useRef(new Map<string, HTMLButtonElement>())
  const filteredRoot = useMemo(
    () => filterTree(tree.root, query.trim().toLocaleLowerCase()) ?? tree.root,
    [query, tree.root]
  )
  const nodes = useMemo(
    () => visibleNodes(filteredRoot, expanded),
    [expanded, filteredRoot]
  )
  const activeNode = findNode(tree.root, activeNodeId) ?? tree.root
  const activeFolder =
    activeNode.node_type === 'root' || activeNode.node_type === 'folder'
      ? activeNode
      : (findNode(tree.root, activeNode.parent_id ?? '') ?? tree.root)
  const excludedMoveTargets = descendantIds(activeNode)
  const moveTargets = allFolders(tree.root).filter(
    (node) =>
      !excludedMoveTargets.has(node.id) && node.id !== activeNode.parent_id
  )

  useEffect(() => {
    const selected = nodes.find(
      ({ node }) => node.document_id === selectedDocumentId
    )
    if (selected) setFocusedId(selected.node.id)
  }, [nodes, selectedDocumentId])

  function focusNode(nodeId: string) {
    setFocusedId(nodeId)
    queueMicrotask(() => itemRefs.current.get(nodeId)?.focus())
  }

  function toggle(node: FileTreeNode, force?: boolean) {
    if (node.node_type !== 'folder') return
    setExpanded((current) => {
      const next = new Set(current)
      const shouldExpand = force ?? !next.has(node.id)
      if (shouldExpand) next.add(node.id)
      else next.delete(node.id)
      return next
    })
  }

  function activate(node: FileTreeNode) {
    setActiveNodeId(node.id)
    setActionError(undefined)
    setCreating(false)
    setMoving(false)
    setConfirmingDelete(false)
    if (node.node_type === 'file' && node.document_id) {
      onSelectDocument(node.document_id)
      return
    }
    onSelectFolder?.(node)
    toggle(node)
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
      if (node.node_type === 'folder' && !expanded.has(node.id)) {
        toggle(node, true)
        return
      }
      const firstChild = node.children[0]
      if (firstChild) focusNode(firstChild.id)
      return
    }
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      if (node.node_type === 'folder' && expanded.has(node.id)) {
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
      activate(node)
      return
    }
    if (event.key === 'F2' && canManage && onRenameNode) {
      event.preventDefault()
      setRenamingId(node.id)
    }
  }

  return (
    <div className='min-w-0'>
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
              setMoving(false)
              setConfirmingDelete(false)
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
      {canManage && activeNode.node_type !== 'root' && (
        <div className='mb-3 flex flex-wrap gap-1 px-2'>
          {onRenameNode && (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              aria-label={t('content.fileTree.renameActionAriaLabel', {
                name: activeNode.name,
              })}
              onClick={() => setRenamingId(activeNode.id)}
            >
              <Pencil />
              {t('content.fileTree.renameAction')}
            </Button>
          )}
          {onMoveNode && (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              aria-label={t('content.fileTree.moveActionAriaLabel', {
                name: activeNode.name,
              })}
              onClick={() => {
                setMoving(true)
                setCreating(false)
                setConfirmingDelete(false)
              }}
            >
              <Move />
              {t('content.fileTree.moveAction')}
            </Button>
          )}
          {onDeleteNode && (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              aria-label={t('content.fileTree.deleteActionAriaLabel', {
                name: activeNode.name,
              })}
              className='text-destructive'
              onClick={() => {
                setConfirmingDelete(true)
                setCreating(false)
                setMoving(false)
              }}
            >
              <Trash2 />
              {t('content.fileTree.deleteAction')}
            </Button>
          )}
        </div>
      )}
      {actionError && (
        <p
          className='mx-2 mb-3 rounded-md bg-destructive/10 p-2 text-destructive text-xs'
          role='alert'
        >
          {actionError}
        </p>
      )}
      {creating && onCreateFolder && (
        <div className='mb-3 px-2'>
          <CreateFolderEditor
            parent={activeFolder}
            onCancel={() => setCreating(false)}
            onCreate={async (name) => {
              try {
                await onCreateFolder(activeFolder, name)
                setCreating(false)
              } catch (error) {
                setActionError(fileTreeActionErrorMessage(error))
              }
            }}
          />
        </div>
      )}
      {moving && onMoveNode && (
        <MoveDialog
          node={activeNode}
          targets={moveTargets}
          onCancel={() => setMoving(false)}
          onMove={onMoveNode}
          onError={setActionError}
        />
      )}
      {confirmingDelete && onDeleteNode && (
        <DeleteDialog
          node={activeNode}
          onCancel={() => setConfirmingDelete(false)}
          onDelete={onDeleteNode}
          onDeleted={() => {
            setConfirmingDelete(false)
            setActiveNodeId(tree.root.id)
          }}
          onError={setActionError}
        />
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
            const isExpanded = isFolder && expanded.has(node.id)
            const selected = node.document_id === selectedDocumentId
            return (
              <div
                key={node.id}
                className='flex min-w-0 items-center'
                style={{
                  paddingInlineStart: `${Math.max(0, level - 1) * 16}px`,
                }}
              >
                {renamingId === node.id && onRenameNode ? (
                  <RenameEditor
                    node={node}
                    onCancel={() => setRenamingId(undefined)}
                    onError={setActionError}
                    onSave={async (name) => {
                      await onRenameNode(node, name)
                      setRenamingId(undefined)
                      focusNode(node.id)
                    }}
                  />
                ) : (
                  <button
                    ref={(element) => {
                      if (element) itemRefs.current.set(node.id, element)
                      else itemRefs.current.delete(node.id)
                    }}
                    type='button'
                    role='treeitem'
                    aria-label={node.name}
                    aria-level={level}
                    aria-selected={selected}
                    aria-expanded={isFolder ? isExpanded : undefined}
                    tabIndex={focusedId === node.id ? 0 : -1}
                    className={cn(
                      'flex h-9 min-w-0 flex-1 items-center gap-1.5 rounded-md px-2 text-left text-sm outline-none transition-colors',
                      'hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50',
                      selected && 'bg-primary/10 text-primary'
                    )}
                    onClick={() => {
                      setFocusedId(node.id)
                      activate(node)
                    }}
                    onKeyDown={(event) => handleKeyDown(event, node, index)}
                  >
                    {isFolder ? (
                      isExpanded ? (
                        <ChevronDown className='size-3.5 shrink-0' />
                      ) : (
                        <ChevronRight className='size-3.5 shrink-0' />
                      )
                    ) : (
                      <span className='size-3.5 shrink-0' />
                    )}
                    {isFolder ? (
                      <Folder className='size-4 shrink-0 text-primary' />
                    ) : (
                      <FileText className='size-4 shrink-0 text-muted-foreground' />
                    )}
                    <span className='truncate'>{node.name}</span>
                  </button>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
