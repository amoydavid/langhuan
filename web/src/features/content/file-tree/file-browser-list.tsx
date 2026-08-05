import { FileText, FolderPlus, MoreHorizontal, Upload } from 'lucide-react'
import { useMemo, useState } from 'react'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { DocumentStatusBadge } from '@/features/documents/document-list'
import type { Document, DocumentStatus } from '@/features/documents/types'
import { formatDateTime } from '@/lib/i18n/datetime'
import type { FileTreeNode } from './schemas'

type FileBrowserListProps = {
  folder: FileTreeNode
  documents: Document[]
  canManage?: boolean
  onOpenFile: (documentId: string) => void
  onUploadFile?: () => void
  onCreateFolder?: () => void
  onRenameFile?: (node: FileTreeNode) => void
  onMoveFile?: (node: FileTreeNode) => void
  onDeleteFile?: (node: FileTreeNode) => void
}

function displayName(item: Document, node: FileTreeNode) {
  return node.name || item.title
}

export function FileBrowserList({
  folder,
  documents,
  canManage = false,
  onOpenFile,
  onUploadFile,
  onCreateFolder,
  onRenameFile,
  onMoveFile,
  onDeleteFile,
}: FileBrowserListProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<DocumentStatus | 'all'>('all')
  const [sort, setSort] = useState<'updated' | 'name'>('updated')
  const documentsByID = useMemo(
    () => new Map(documents.map((item) => [item.id, item])),
    [documents]
  )
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const rows = folder.children
    .filter(
      (node): node is FileTreeNode & { document_id: string } =>
        node.node_type === 'file' && Boolean(node.document_id)
    )
    .flatMap((node) => {
      const item = documentsByID.get(node.document_id)
      return item ? [{ item, node }] : []
    })
    .filter(({ item, node }) =>
      normalizedQuery.length === 0
        ? true
        : displayName(item, node).toLocaleLowerCase().includes(normalizedQuery)
    )
    .filter(({ item }) => status === 'all' || item.status === status)
    .sort((left, right) =>
      sort === 'name'
        ? displayName(left.item, left.node).localeCompare(
            displayName(right.item, right.node),
            'zh-CN'
          )
        : right.item.updated_at.localeCompare(left.item.updated_at)
    )

  return (
    <section
      className='flex min-h-0 flex-1 flex-col gap-3'
      aria-label={t('content.fileBrowser.label')}
    >
      <div className='flex shrink-0 flex-wrap items-center justify-between gap-2'>
        <p className='min-w-0 truncate font-medium text-sm'>
          {t('content.fileBrowser.currentFolder', { path: folder.path || '/' })}
        </p>
        {canManage && (
          <div className='flex gap-2'>
            <Button type='button' size='sm' onClick={onUploadFile}>
              <Upload />
              {t('content.fileBrowser.uploadFile')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={onCreateFolder}
            >
              <FolderPlus />
              {t('content.fileBrowser.createFolder')}
            </Button>
          </div>
        )}
      </div>

      <div className='flex shrink-0 flex-wrap gap-2'>
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          aria-label={t('content.fileBrowser.searchAriaLabel')}
          placeholder={t('content.fileBrowser.searchPlaceholder')}
          className='h-9 min-w-52 flex-1 sm:max-w-sm'
        />
        <Select
          value={status}
          onValueChange={(value) => setStatus(value as DocumentStatus | 'all')}
        >
          <SelectTrigger
            className='h-9 w-32'
            aria-label={t('content.fileBrowser.statusAriaLabel')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>
              {t('content.fileBrowser.allStatuses')}
            </SelectItem>
            <SelectItem value='ready'>
              {t('content.contentList.statusReady')}
            </SelectItem>
            <SelectItem value='processing'>
              {t('content.contentList.statusProcessing')}
            </SelectItem>
            <SelectItem value='failed'>
              {t('content.contentList.statusFailed')}
            </SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={sort}
          onValueChange={(value) => setSort(value as 'updated' | 'name')}
        >
          <SelectTrigger
            className='h-9 w-36'
            aria-label={t('content.fileBrowser.sortAriaLabel')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='updated'>
              {t('content.fileBrowser.sortUpdated')}
            </SelectItem>
            <SelectItem value='name'>
              {t('content.fileBrowser.sortName')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      {rows.length === 0 ? (
        <div className='flex min-h-48 flex-1 flex-col items-center justify-center rounded-xl border border-dashed bg-muted/15 text-center'>
          <FileText className='mb-3 size-6 text-muted-foreground' />
          <p className='font-medium text-sm'>
            {t('content.fileBrowser.emptyTitle')}
          </p>
          <p className='mt-1 max-w-sm text-muted-foreground text-sm'>
            {t('content.fileBrowser.emptyDescription')}
          </p>
        </div>
      ) : (
        <div className='min-h-0 flex-1 overflow-auto rounded-xl border'>
          <table className='w-full table-fixed text-sm'>
            <thead className='sticky top-0 z-10 bg-card text-left text-muted-foreground'>
              <tr className='border-b'>
                <th className='w-[56%] px-4 py-3 font-medium'>
                  {t('content.fileBrowser.columnName')}
                </th>
                <th className='hidden w-28 px-4 py-3 font-medium sm:table-cell'>
                  {t('content.fileBrowser.columnStatus')}
                </th>
                <th className='hidden w-36 px-4 py-3 font-medium lg:table-cell'>
                  {t('content.fileBrowser.columnUpdatedAt')}
                </th>
                <th className='w-14 px-3 py-3 text-right font-medium'>
                  {t('content.fileBrowser.columnActions')}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ item, node }) => {
                const name = displayName(item, node)
                return (
                  <tr
                    key={node.id}
                    className='group border-b last:border-b-0 hover:bg-muted/40'
                  >
                    <td className='min-w-0 px-4 py-3'>
                      <button
                        type='button'
                        className='flex w-full min-w-0 items-center gap-2 text-left font-medium outline-none hover:text-primary focus-visible:ring-2 focus-visible:ring-ring/50'
                        onClick={() => onOpenFile(item.id)}
                      >
                        <FileText className='size-4 shrink-0 text-primary' />
                        <span className='truncate'>{name}</span>
                      </button>
                    </td>
                    <td className='hidden whitespace-nowrap px-4 py-3 sm:table-cell'>
                      <DocumentStatusBadge status={item.status} />
                    </td>
                    <td className='hidden whitespace-nowrap px-4 py-3 text-muted-foreground lg:table-cell'>
                      {formatDateTime(item.updated_at, {
                        dateStyle: 'short',
                        timeStyle: 'short',
                      })}
                    </td>
                    <td className='px-3 py-3 text-right'>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            aria-label={t(
                              'content.fileBrowser.rowActionsAriaLabel',
                              { name }
                            )}
                          >
                            <MoreHorizontal />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align='end'>
                          <DropdownMenuItem onClick={() => onOpenFile(item.id)}>
                            {t('content.fileBrowser.viewFile')}
                          </DropdownMenuItem>
                          {canManage && onRenameFile && (
                            <DropdownMenuItem
                              onClick={() => onRenameFile(node)}
                            >
                              {t('content.fileTree.renameAction')}
                            </DropdownMenuItem>
                          )}
                          {canManage && onMoveFile && (
                            <DropdownMenuItem onClick={() => onMoveFile(node)}>
                              {t('content.fileTree.moveAction')}
                            </DropdownMenuItem>
                          )}
                          {canManage && onDeleteFile && (
                            <>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                variant='destructive'
                                onClick={() => onDeleteFile(node)}
                              >
                                {t('content.fileTree.deleteAction')}
                              </DropdownMenuItem>
                            </>
                          )}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
