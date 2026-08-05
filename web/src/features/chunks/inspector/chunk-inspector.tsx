import type { TFunction } from 'i18next'
import {
  CheckCircle2,
  ChevronDown,
  CircleOff,
  Pencil,
  Scissors,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import type { Chunk } from '@/features/chunks/types'
import type { DocumentKind } from '@/features/documents/types'
import i18n from '@/lib/i18n'
import { formatDateTime } from '@/lib/i18n/datetime'

const PAGE_SIZE = 20

type ChunkInspectorProps = {
  documentTitle: string
  documentKind: DocumentKind
  chunks: Chunk[]
  /** 当前查看的 chunk id（来自 URL，用于高亮 + 自动滚入视图）。 */
  selectedChunkId?: string
  /** 当前页码（1-based）。 */
  page: number
  canEdit: boolean
  /** 切换查看的 chunk。 */
  onSelectChunk?: (chunkId: string) => void
  /** 翻页。 */
  onPageChange?: (page: number) => void
  /** 点击编辑（来自卡片编辑按钮）。 */
  onEdit?: (chunk: Chunk) => void
}

function numberField(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

export function anchorLabel(anchor: Record<string, unknown>) {
  const lineStart = numberField(anchor.line_start)
  const lineEnd = numberField(anchor.line_end)
  if (lineStart !== undefined && lineEnd !== undefined) {
    return i18n.t('chunks.inspector.anchor.range', {
      start: lineStart,
      end: lineEnd,
    })
  }
  const rowStart = numberField(anchor.row_start)
  const rowEnd = numberField(anchor.row_end)
  const sheet = typeof anchor.sheet === 'string' ? anchor.sheet : undefined
  if (rowStart !== undefined && rowEnd !== undefined) {
    return sheet
      ? i18n.t('chunks.inspector.anchor.rangeWithSheet', {
          sheet,
          start: rowStart,
          end: rowEnd,
        })
      : i18n.t('chunks.inspector.anchor.range', {
          start: rowStart,
          end: rowEnd,
        })
  }
  return i18n.t('chunks.inspector.anchor.unknown')
}

export function revisionStatusLabel(t: TFunction) {
  return {
    pending: t('chunks.inspector.revisionStatus.pending'),
    indexing: t('chunks.inspector.revisionStatus.indexing'),
    ready: t('chunks.inspector.revisionStatus.ready'),
    failed: t('chunks.inspector.revisionStatus.failed'),
  } as const
}

function previewText(chunk: Chunk, t: TFunction) {
  const content = chunk.active_revision?.content
  if (!content) return t('chunks.inspector.list.cardNoContent')
  return content.replace(/\s+/g, ' ').trim()
}

export function ChunkInspector({
  documentTitle,
  documentKind,
  chunks,
  selectedChunkId,
  page,
  canEdit,
  onSelectChunk,
  onPageChange,
  onEdit,
}: ChunkInspectorProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const listRef = useRef<HTMLDivElement>(null)

  // 本地按 context_header / 内容前缀过滤；筛选后再分页。
  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    if (!keyword) return chunks
    return chunks.filter((chunk) => {
      const header = chunk.active_revision?.context_header ?? ''
      const content = chunk.active_revision?.content ?? ''
      return (
        header.toLowerCase().includes(keyword) ||
        content.toLowerCase().includes(keyword) ||
        chunk.source_content.toLowerCase().includes(keyword)
      )
    })
  }, [chunks, query])

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const safePage = Math.min(Math.max(1, page), totalPages)
  const startIdx = (safePage - 1) * PAGE_SIZE
  const pageItems = filtered.slice(startIdx, startIdx + PAGE_SIZE)

  useEffect(() => {
    if (!selectedChunkId) return
    listRef.current
      ?.querySelector<HTMLButtonElement>(
        `[data-chunk-card="${selectedChunkId}"]`
      )
      ?.scrollIntoView({ block: 'nearest' })
  }, [selectedChunkId])

  if (chunks.length === 0) {
    return (
      <div className='rounded-xl border border-dashed p-6 text-center text-muted-foreground text-sm'>
        {t('chunks.inspector.emptyState')}
      </div>
    )
  }

  const countLabel = query
    ? t('chunks.inspector.list.countFiltered', { count: filtered.length })
    : t('chunks.inspector.list.countTotal', { count: chunks.length })

  return (
    <section
      className='flex min-w-0 flex-col gap-3'
      aria-label={t('chunks.inspector.ariaLabel')}
    >
      <div className='flex items-center justify-between gap-2'>
        <h2 className='font-semibold text-base'>{documentTitle}</h2>
        <span className='text-muted-foreground text-xs'>{countLabel}</span>
      </div>

      {documentKind === 'faq' && (
        <div className='rounded-lg bg-muted/50 p-3 text-muted-foreground text-sm'>
          {t('chunks.inspector.faqNotice')}
        </div>
      )}

      <Input
        value={query}
        onChange={(event) => {
          setQuery(event.target.value)
          onPageChange?.(1)
        }}
        placeholder={t('chunks.inspector.list.searchPlaceholder')}
        className='h-9'
      />

      <div ref={listRef} className='flex-1 space-y-2 overflow-y-auto'>
        {pageItems.length === 0 ? (
          <p className='py-8 text-center text-muted-foreground text-sm'>
            {countLabel}
          </p>
        ) : (
          renderHierarchy(pageItems).map((item) => {
            if (item.kind === 'parent') {
              return (
                <ParentChunkGroup
                  key={item.parent.id}
                  parent={item.parent}
                  childChunks={item.children}
                  selectedChunkId={selectedChunkId}
                  canEdit={canEdit && documentKind !== 'faq'}
                  onSelectChunk={onSelectChunk}
                  onEdit={onEdit}
                />
              )
            }
            const chunk = item.chunk
            const revision = chunk.active_revision
            const isSelected = chunk.id === selectedChunkId
            return (
              <ChunkCard
                key={chunk.id}
                chunk={chunk}
                selected={isSelected}
                canEdit={
                  canEdit &&
                  documentKind !== 'faq' &&
                  Boolean(revision) &&
                  chunk.role !== 'parent'
                }
                onSelect={() => onSelectChunk?.(chunk.id)}
                onEdit={() => onEdit?.(chunk)}
              />
            )
          })
        )}
      </div>

      {totalPages > 1 && (
        <Pagination
          page={safePage}
          totalPages={totalPages}
          onChange={onPageChange}
        />
      )}
    </section>
  )
}

type HierarchyItem =
  | { kind: 'parent'; parent: Chunk; children: Chunk[] }
  | { kind: 'flat'; chunk: Chunk }

function renderHierarchy(chunks: Chunk[]): HierarchyItem[] {
  const parents = new Map<string, Chunk>()
  const children = new Map<string, Chunk[]>()
  const flat: Chunk[] = []
  for (const chunk of chunks) {
    if (chunk.role === 'parent') {
      parents.set(chunk.id, chunk)
      continue
    }
    if (chunk.role === 'child' && chunk.parent_chunk_id) {
      const group = children.get(chunk.parent_chunk_id) ?? []
      group.push(chunk)
      children.set(chunk.parent_chunk_id, group)
      continue
    }
    flat.push(chunk)
  }
  const result: HierarchyItem[] = []
  for (const parent of parents.values()) {
    result.push({
      kind: 'parent',
      parent,
      children: children.get(parent.id) ?? [],
    })
    children.delete(parent.id)
  }
  for (const orphaned of children.values()) flat.push(...orphaned)
  flat.sort((left, right) => left.sequence - right.sequence)
  return [...result, ...flat.map((chunk) => ({ kind: 'flat' as const, chunk }))]
}

type ParentChunkGroupProps = {
  parent: Chunk
  childChunks: Chunk[]
  selectedChunkId?: string
  canEdit: boolean
  onSelectChunk?: (chunkId: string) => void
  onEdit?: (chunk: Chunk) => void
}

function ParentChunkGroup({
  parent,
  childChunks,
  selectedChunkId,
  canEdit,
  onSelectChunk,
  onEdit,
}: ParentChunkGroupProps) {
  const { t } = useTranslation()
  return (
    <Collapsible
      defaultOpen={childChunks.some((child) => child.id === selectedChunkId)}
      className='rounded-xl border bg-card'
    >
      <CollapsibleTrigger className='flex min-h-11 w-full items-center gap-2 px-3 text-left font-medium text-sm'>
        <ChevronDown className='size-4 transition-transform [[data-state=closed]_&]:-rotate-90' />
        {t('chunks.inspector.parentGroup', {
          sequence: parent.sequence + 1,
          count: childChunks.length,
        })}
      </CollapsibleTrigger>
      <CollapsibleContent className='space-y-2 border-t p-2'>
        <p className='px-1 text-muted-foreground text-xs'>
          {t('chunks.inspector.parentReadOnly')}
        </p>
        <ChunkCard
          chunk={parent}
          selected={parent.id === selectedChunkId}
          canEdit={false}
          onSelect={() => onSelectChunk?.(parent.id)}
          onEdit={() => {}}
        />
        {childChunks.map((child) => (
          <ChunkCard
            key={child.id}
            chunk={child}
            selected={child.id === selectedChunkId}
            canEdit={canEdit && Boolean(child.active_revision)}
            onSelect={() => onSelectChunk?.(child.id)}
            onEdit={() => onEdit?.(child)}
          />
        ))}
      </CollapsibleContent>
    </Collapsible>
  )
}

type ChunkCardProps = {
  chunk: Chunk
  selected: boolean
  canEdit: boolean
  onSelect: () => void
  onEdit: () => void
}

function ChunkCard({
  chunk,
  selected,
  canEdit,
  onSelect,
  onEdit,
}: ChunkCardProps) {
  const { t } = useTranslation()
  const revision = chunk.active_revision
  const enabled = revision?.enabled ?? false
  const updated = formatDateTime(revision?.created_at ?? chunk.created_at, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })

  return (
    <div
      data-chunk-card={chunk.id}
      className={[
        'group rounded-xl border bg-card p-3 transition-colors',
        selected
          ? 'border-primary/60 ring-1 ring-primary/30'
          : 'hover:bg-muted/40',
      ].join(' ')}
    >
      <div className='flex items-start gap-2'>
        <button
          type='button'
          onClick={onSelect}
          aria-label={t('chunks.inspector.list.cardAriaLabel', {
            sequence: chunk.sequence,
          })}
          aria-pressed={selected}
          className='min-w-0 flex-1 cursor-pointer text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50'
        >
          <span className='flex items-center gap-2'>
            {enabled ? (
              <CheckCircle2
                className='size-4 shrink-0 text-success'
                aria-hidden='true'
              />
            ) : (
              <CircleOff
                className='size-4 shrink-0 text-muted-foreground'
                aria-hidden='true'
              />
            )}
            <span className='shrink-0 font-mono text-muted-foreground text-xs'>
              #{chunk.sequence}
            </span>
            <span className='truncate font-medium text-sm'>
              {revision?.context_header ||
                t('chunks.inspector.chunkTabNoTitle')}
            </span>
          </span>
          <span className='mt-1.5 line-clamp-2 block text-muted-foreground text-xs'>
            {previewText(chunk, t)}
          </span>
          <span className='mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-muted-foreground text-xs'>
            <span className='inline-flex items-center gap-1'>
              <Scissors className='size-3' aria-hidden='true' />
              {anchorLabel(chunk.source_anchor)}
            </span>
            <span aria-hidden='true'>·</span>
            <span>
              {t('chunks.inspector.list.cardMetaRevision', {
                number: revision?.revision_no ?? 0,
              })}
            </span>
            <span aria-hidden='true'>·</span>
            <span>
              {t('chunks.inspector.list.cardMetaUpdated', { time: updated })}
            </span>
          </span>
        </button>
        {canEdit && (
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='size-7 shrink-0 opacity-60 group-hover:opacity-100'
            aria-label={t('chunks.inspector.list.cardEditAriaLabel', {
              sequence: chunk.sequence,
            })}
            onClick={onEdit}
          >
            <Pencil className='size-3.5' />
          </Button>
        )}
      </div>
    </div>
  )
}

type PaginationProps = {
  page: number
  totalPages: number
  onChange?: (page: number) => void
}

function buildPageList(page: number, totalPages: number): (number | '…')[] {
  const pages: (number | '…')[] = []
  const add = (value: number) => {
    if (!pages.includes(value)) pages.push(value)
  }
  add(1)
  if (page - 1 > 2) pages.push('…')
  for (let p = page - 1; p <= page + 1; p++) {
    if (p > 1 && p < totalPages) add(p)
  }
  if (totalPages - page > 2) pages.push('…')
  if (totalPages > 1) add(totalPages)
  return pages
}

function Pagination({ page, totalPages, onChange }: PaginationProps) {
  const { t } = useTranslation()
  const pages = buildPageList(page, totalPages)

  return (
    <nav
      className='flex items-center justify-center gap-1'
      aria-label={t('chunks.inspector.list.pageAriaLabel')}
    >
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={page <= 1}
        onClick={() => onChange?.(page - 1)}
      >
        {t('chunks.inspector.list.pagePrevious')}
      </Button>
      {pages.map((value, index) =>
        value === '…' ? (
          <span
            key={`gap-${index}`}
            className='px-1 text-muted-foreground text-sm'
          >
            …
          </span>
        ) : (
          <Button
            key={value}
            type='button'
            variant={value === page ? 'default' : 'outline'}
            size='sm'
            className='min-w-8'
            aria-current={value === page ? 'page' : undefined}
            aria-label={t('chunks.inspector.list.pageCurrent', {
              page: value,
            })}
            onClick={() => onChange?.(value)}
          >
            {value}
          </Button>
        )
      )}
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={page >= totalPages}
        onClick={() => onChange?.(page + 1)}
      >
        {t('chunks.inspector.list.pageNext')}
      </Button>
    </nav>
  )
}
