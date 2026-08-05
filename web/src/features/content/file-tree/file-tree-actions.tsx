import { zodResolver } from '@hookform/resolvers/zod'
import { ChevronDown, ChevronRight, Folder, FolderOpen } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { FileTreeNode } from './schemas'

const renameSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, i18n.t('content.fileTree.rename.nameRequired'))
    .max(255, i18n.t('content.fileTree.rename.nameTooLong')),
})

export function fileTreeActionErrorMessage(error: unknown) {
  const apiError = parseApiError(error)
  if (apiError.code === 'file_tree_not_empty') {
    return i18n.t('content.fileTree.errors.notEmpty')
  }
  if (apiError.code === 'file_tree_name_conflict') {
    return i18n.t('content.fileTree.errors.nameConflict')
  }
  if (apiError.code === 'file_tree_cycle') {
    return i18n.t('content.fileTree.errors.cycle')
  }
  return apiError.message
}

export function CreateFolderDialog({
  parent,
  open,
  onOpenChange,
  onCreate,
}: {
  parent: FileTreeNode
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreate: (name: string) => Promise<void> | void
}) {
  const { t } = useTranslation()
  const form = useForm<z.infer<typeof renameSchema>>({
    resolver: zodResolver(renameSchema),
    defaultValues: { name: '' },
  })
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t('content.fileTree.createFolder.modalTitle')}
          </DialogTitle>
          <DialogDescription>
            {t('content.fileTree.createFolder.formAriaLabel', {
              path: parent.path || '/',
            })}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form
            className='space-y-4'
            onSubmit={form.handleSubmit(async ({ name }) => {
              await onCreate(name.trim())
              form.reset()
              onOpenChange(false)
            })}
          >
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      {...field}
                      autoFocus
                      aria-label={t(
                        'content.fileTree.createFolder.nameAriaLabel'
                      )}
                      placeholder={t(
                        'content.fileTree.createFolder.namePlaceholder'
                      )}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => onOpenChange(false)}
              >
                {t('content.fileTree.createFolder.cancel')}
              </Button>
              <Button
                type='submit'
                size='sm'
                disabled={form.formState.isSubmitting}
              >
                {t('content.fileTree.createFolder.create')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

export function RenameDialog({
  node,
  open,
  onOpenChange,
  onSave,
  onError,
}: {
  node: FileTreeNode
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (name: string) => Promise<void> | void
  onError: (message: string) => void
}) {
  const { t } = useTranslation()
  const form = useForm<z.infer<typeof renameSchema>>({
    resolver: zodResolver(renameSchema),
    defaultValues: { name: node.name },
  })
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('content.fileTree.rename.modalTitle')}</DialogTitle>
          <DialogDescription>{node.name}</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form
            className='space-y-4'
            onSubmit={form.handleSubmit(async ({ name }) => {
              try {
                await onSave(name.trim())
                onOpenChange(false)
              } catch (error) {
                onError(fileTreeActionErrorMessage(error))
              }
            })}
          >
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <Input
                      {...field}
                      autoFocus
                      aria-label={t('content.fileTree.rename.inputAriaLabel', {
                        name: node.name,
                      })}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => onOpenChange(false)}
              >
                {t('content.fileTree.rename.cancel')}
              </Button>
              <Button
                type='submit'
                size='sm'
                disabled={form.formState.isSubmitting}
              >
                {t('content.fileTree.rename.save')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

type MoveTarget = FileTreeNode

function MoveTreePicker({
  root,
  excluded,
  selectedId,
  onSelect,
}: {
  root: FileTreeNode
  excluded: ReadonlySet<string>
  selectedId: string | undefined
  onSelect: (node: MoveTarget) => void
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set([root.id])
  )

  function toggle(node: FileTreeNode) {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(node.id)) next.delete(node.id)
      else next.add(node.id)
      return next
    })
  }

  function renderNode(node: FileTreeNode, level: number) {
    if (node.node_type === 'file') return null
    const isExcluded = excluded.has(node.id)
    const isExpanded = expanded.has(node.id)
    const isSelected = node.id === selectedId
    const hasChildren = node.children.some((c) => c.node_type !== 'file')
    return (
      <div key={node.id}>
        <div
          className='flex items-center gap-1 rounded-md py-1.5 pr-2 text-sm'
          style={{ paddingInlineStart: `${Math.max(0, level) * 16}px` }}
        >
          {hasChildren ? (
            <button
              type='button'
              className='flex size-5 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground'
              onClick={() => toggle(node)}
              aria-label={isExpanded ? '收起' : '展开'}
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
            type='button'
            disabled={isExcluded}
            onClick={() => !isExcluded && onSelect(node)}
            className={cn(
              'flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-2 py-1 text-left outline-none transition-colors',
              isExcluded
                ? 'cursor-not-allowed text-muted-foreground/50'
                : 'hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50',
              isSelected && 'bg-primary/10 text-primary'
            )}
          >
            {isExpanded ? (
              <FolderOpen className='size-4 shrink-0 text-primary' />
            ) : (
              <Folder className='size-4 shrink-0 text-primary' />
            )}
            <span className='truncate'>
              {node.name}
              {node.node_type === 'root' &&
                ` (${t('content.fileTree.treeAriaLabel')})`}
            </span>
          </button>
        </div>
        {isExpanded &&
          node.children.map((child) => renderNode(child, level + 1))}
      </div>
    )
  }

  return (
    <div className='max-h-72 overflow-y-auto rounded-lg border bg-muted/20 p-2'>
      {renderNode(root, 0)}
    </div>
  )
}

export function MoveDialog({
  node,
  root,
  open,
  onOpenChange,
  onMove,
  onError,
}: {
  node: FileTreeNode
  root: FileTreeNode
  open: boolean
  onOpenChange: (open: boolean) => void
  onMove: (node: FileTreeNode, target: FileTreeNode) => Promise<void> | void
  onError: (message: string) => void
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<FileTreeNode>()
  const excluded = useMemo(() => {
    const ids = new Set<string>()
    function collect(n: FileTreeNode) {
      ids.add(n.id)
      n.children.forEach(collect)
    }
    collect(node)
    return ids
  }, [node])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t('content.fileTree.move.modalTitle', { name: node.name })}
          </DialogTitle>
          <DialogDescription>
            {t('content.fileTree.move.currentLocation', {
              path: node.path || '/',
            })}
          </DialogDescription>
        </DialogHeader>
        <MoveTreePicker
          root={root}
          excluded={excluded}
          selectedId={selected?.id}
          onSelect={setSelected}
        />
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => onOpenChange(false)}
          >
            {t('content.fileTree.move.cancel')}
          </Button>
          <Button
            type='button'
            size='sm'
            disabled={!selected}
            onClick={async () => {
              if (!selected) return
              try {
                await onMove(node, selected)
                onOpenChange(false)
              } catch (error) {
                onError(fileTreeActionErrorMessage(error))
              }
            }}
          >
            {selected
              ? t('content.fileTree.move.confirm', {
                  path: selected.path || '/',
                })
              : t('content.fileTree.move.title')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function DeleteDialog({
  node,
  open,
  onOpenChange,
  onDelete,
  onDeleted,
  onError,
}: {
  node: FileTreeNode
  open: boolean
  onOpenChange: (open: boolean) => void
  onDelete: (node: FileTreeNode) => Promise<void> | void
  onDeleted: () => void
  onError: (message: string) => void
}) {
  const { t } = useTranslation()
  const isFile = node.node_type === 'file'
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('content.fileTree.delete.modalTitle')}</DialogTitle>
          <DialogDescription>
            {isFile
              ? t('content.fileTree.delete.fileDescription')
              : t('content.fileTree.delete.folderDescription')}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='rounded-lg border bg-muted/20 p-3'>
            <p className='font-medium text-sm'>{node.name}</p>
            <p className='mt-0.5 truncate text-muted-foreground text-xs'>
              {t('content.fileTree.delete.filePath', {
                path: node.path || '/',
              })}
            </p>
          </div>
          <p className='text-muted-foreground text-sm'>
            {isFile
              ? t('content.fileTree.delete.fileWarning')
              : t('content.fileTree.delete.folderWarning')}
          </p>
        </div>
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => onOpenChange(false)}
          >
            {t('content.fileTree.delete.cancel')}
          </Button>
          <Button
            type='button'
            variant='destructive'
            size='sm'
            onClick={async () => {
              try {
                await onDelete(node)
                onDeleted()
                onOpenChange(false)
              } catch (error) {
                onError(fileTreeActionErrorMessage(error))
              }
            }}
          >
            {t('content.fileTree.delete.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
