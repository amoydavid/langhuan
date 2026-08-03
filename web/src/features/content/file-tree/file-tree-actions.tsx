import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
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

export function CreateFolderEditor({
  parent,
  onCancel,
  onCreate,
}: {
  parent: FileTreeNode
  onCancel: () => void
  onCreate: (name: string) => Promise<void> | void
}) {
  const { t } = useTranslation()
  const form = useForm<z.infer<typeof renameSchema>>({
    resolver: zodResolver(renameSchema),
    defaultValues: { name: '' },
  })
  return (
    <Form {...form}>
      <form
        className='space-y-3 rounded-lg border bg-muted/20 p-3'
        aria-label={t('content.fileTree.createFolder.formAriaLabel', {
          path: parent.path || '/',
        })}
        onSubmit={form.handleSubmit(async ({ name }) => onCreate(name.trim()))}
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
                  aria-label={t('content.fileTree.createFolder.nameAriaLabel')}
                  placeholder={t(
                    'content.fileTree.createFolder.namePlaceholder'
                  )}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className='flex justify-end gap-2'>
          <Button type='button' variant='ghost' size='sm' onClick={onCancel}>
            {t('content.fileTree.createFolder.cancel')}
          </Button>
          <Button
            type='submit'
            size='sm'
            disabled={form.formState.isSubmitting}
          >
            {t('content.fileTree.createFolder.create')}
          </Button>
        </div>
      </form>
    </Form>
  )
}

export function RenameEditor({
  node,
  onCancel,
  onSave,
  onError,
}: {
  node: FileTreeNode
  onCancel: () => void
  onSave: (name: string) => Promise<void> | void
  onError: (message: string) => void
}) {
  const { t } = useTranslation()
  const form = useForm<z.infer<typeof renameSchema>>({
    resolver: zodResolver(renameSchema),
    defaultValues: { name: node.name },
  })
  return (
    <Form {...form}>
      <form
        className='min-w-0 flex-1'
        onSubmit={form.handleSubmit(async ({ name }) => {
          try {
            await onSave(name.trim())
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
                  className='h-7 px-2 text-sm'
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                      event.preventDefault()
                      onCancel()
                    }
                  }}
                  onBlur={() => {
                    if (!form.formState.isSubmitting) onCancel()
                  }}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </form>
    </Form>
  )
}

export function MoveDialog({
  node,
  targets,
  onCancel,
  onMove,
  onError,
}: {
  node: FileTreeNode
  targets: FileTreeNode[]
  onCancel: () => void
  onMove: (node: FileTreeNode, target: FileTreeNode) => Promise<void> | void
  onError: (message: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div
      role='dialog'
      aria-label={t('content.fileTree.move.dialogAriaLabel')}
      className='mx-2 mb-3 space-y-2 rounded-lg border bg-background p-3'
    >
      <p className='font-medium text-sm'>{t('content.fileTree.move.title')}</p>
      <div className='grid gap-1'>
        {targets.map((target) => (
          <Button
            key={target.id}
            type='button'
            variant='ghost'
            className='justify-start'
            onClick={async () => {
              try {
                await onMove(node, target)
                onCancel()
              } catch (error) {
                onError(fileTreeActionErrorMessage(error))
              }
            }}
          >
            {t('content.fileTree.move.toTarget', {
              path: target.path || '/',
            })}
          </Button>
        ))}
      </div>
      <Button type='button' variant='outline' size='sm' onClick={onCancel}>
        {t('content.fileTree.move.cancel')}
      </Button>
    </div>
  )
}

export function DeleteDialog({
  node,
  onCancel,
  onDelete,
  onDeleted,
  onError,
}: {
  node: FileTreeNode
  onCancel: () => void
  onDelete: (node: FileTreeNode) => Promise<void> | void
  onDeleted: () => void
  onError: (message: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div
      role='alertdialog'
      aria-label={t('content.fileTree.delete.dialogAriaLabel', {
        name: node.name,
      })}
      className='mx-2 mb-3 space-y-3 rounded-lg border border-destructive/30 bg-destructive/5 p-3'
    >
      <p className='font-medium text-sm'>
        {t('content.fileTree.delete.title', { name: node.name })}
      </p>
      <p className='text-muted-foreground text-xs'>
        {node.node_type === 'file'
          ? t('content.fileTree.delete.fileDescription')
          : t('content.fileTree.delete.folderDescription')}
      </p>
      <div className='flex justify-end gap-2'>
        <Button type='button' variant='outline' size='sm' onClick={onCancel}>
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
            } catch (error) {
              onError(fileTreeActionErrorMessage(error))
            }
          }}
        >
          {t('content.fileTree.delete.confirm')}
        </Button>
      </div>
    </div>
  )
}
