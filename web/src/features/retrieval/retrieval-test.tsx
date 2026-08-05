import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { type FormEvent, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { SafeMarkdown } from '@/components/safe-markdown'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'
import { retrievalTestQueryOptions } from './queries'
import type { RetrievalRequest, RetrievalResult } from './types'

const optionalTopK = z
  .union([z.number(), z.string().transform((value) => Number(value))])
  .optional()
  .pipe(z.number().int().min(1).max(200).optional())

export const retrievalSearchSchema = z.object({
  q: z.string().trim().optional(),
  vectorTopK: optionalTopK,
  keywordTopK: optionalTopK,
  finalTopK: z
    .union([z.number(), z.string().transform((value) => Number(value))])
    .optional()
    .pipe(z.number().int().min(1).max(50).optional()),
  chunk: z.uuid().optional(),
})

export type RetrievalSearch = z.infer<typeof retrievalSearchSchema>

type RetrievalDefaults = {
  fts_config: string
  vector_top_k: number
  keyword_top_k: number
  final_top_k: number
  rrf_k: number
}

export function toRetrievalRequest(
  search: RetrievalSearch,
  defaults: RetrievalDefaults
): RetrievalRequest {
  return {
    query: search.q ?? '',
    vector_top_k: search.vectorTopK ?? defaults.vector_top_k,
    keyword_top_k: search.keywordTopK ?? defaults.keyword_top_k,
    final_top_k: search.finalTopK ?? defaults.final_top_k,
  }
}

type RetrievalTestProps = {
  workspaceSlug: string
  kbId: string
  search: RetrievalSearch
  defaults: RetrievalDefaults
  activeGenerationLabel?: string
  /**
   * 可注入的检索查询钩子，默认使用基于 TanStack Query 的真实实现。
   * 测试可传入 stub 以避免浏览器 mock 的不稳定性。
   */
  useResults?: (
    workspaceSlug: string,
    kbId: string,
    request: RetrievalRequest | null
  ) => { data?: RetrievalResult[]; isFetching: boolean; error: unknown }
}

function sourceAnchorLabel(anchor: Record<string, unknown>) {
  const lineStart =
    typeof anchor.line_start === 'number' ? anchor.line_start : 0
  const lineEnd = typeof anchor.line_end === 'number' ? anchor.line_end : 0
  if (lineStart > 0 && lineEnd >= lineStart)
    return i18n.t('retrieval.test.anchor.lineRange', {
      start: lineStart,
      end: lineEnd,
    })

  const sheet = typeof anchor.sheet === 'string' ? anchor.sheet : ''
  const rowStart = typeof anchor.row_start === 'number' ? anchor.row_start : 0
  const rowEnd = typeof anchor.row_end === 'number' ? anchor.row_end : 0
  if (sheet && rowStart > 0) {
    const linePart =
      rowEnd >= rowStart
        ? i18n.t('retrieval.test.anchor.lineRange', {
            start: rowStart,
            end: rowEnd,
          })
        : i18n.t('retrieval.test.anchor.lineSingle', { start: rowStart })
    return i18n.t('retrieval.test.anchor.sheetLine', { sheet, line: linePart })
  }

  const paragraphStart =
    typeof anchor.paragraph_start === 'number' ? anchor.paragraph_start : 0
  const paragraphEnd =
    typeof anchor.paragraph_end === 'number' ? anchor.paragraph_end : 0
  if (paragraphStart > 0) {
    return paragraphEnd >= paragraphStart
      ? i18n.t('retrieval.test.anchor.paragraphRange', {
          start: paragraphStart,
          end: paragraphEnd,
        })
      : i18n.t('retrieval.test.anchor.paragraphSingle', {
          start: paragraphStart,
        })
  }
  return i18n.t('retrieval.test.anchor.unknown')
}

function resultBasePath(
  workspaceSlug: string,
  kbId: string,
  result: RetrievalResult
) {
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content`
  if (result.document_kind === 'faq') {
    return `${base}/faq/${encodeURIComponent(result.document_id)}`
  }
  if (result.document_kind === 'web') {
    return `${base}/web/${encodeURIComponent(result.document_id)}`
  }
  return `${base}/files/${encodeURIComponent(result.document_id)}`
}

function score(value: number | undefined) {
  return value === undefined ? '—' : String(value)
}

// useTrackDuration 在 startedAt 设置后、isFetching 从 true 变为 false 时
// 回调一次，传入本次请求耗时（毫秒）。startedAt 为 null 时不触发。
function useTrackDuration(
  startedAt: number | null,
  isFetching: boolean,
  onComplete: (elapsedMs: number) => void
) {
  const [wasFetching, setWasFetching] = useState(false)
  useEffect(() => {
    if (startedAt === null) {
      setWasFetching(false)
      return
    }
    if (isFetching) {
      setWasFetching(true)
      return
    }
    if (wasFetching) {
      onComplete(Date.now() - startedAt)
      setWasFetching(false)
    }
  }, [startedAt, isFetching, wasFetching, onComplete])
}

function formatDuration(ms: number) {
  if (ms < 1000) return i18n.t('retrieval.test.durationMs', { ms })
  return i18n.t('retrieval.test.durationSec', {
    seconds: (ms / 1000).toFixed(2),
  })
}

// useDefaultRetrievalResults 是基于 TanStack Query 的真实检索实现。
export function useDefaultRetrievalResults(
  workspaceSlug: string,
  kbId: string,
  request: RetrievalRequest | null
) {
  const enabled = request !== null && request.query.trim().length > 0
  const retrieval = useQuery({
    ...retrievalTestQueryOptions(
      workspaceSlug,
      kbId,
      request ?? {
        query: '',
        vector_top_k: 0,
        keyword_top_k: 0,
        final_top_k: 0,
      }
    ),
    enabled,
  })
  return {
    data: retrieval.data,
    isFetching: retrieval.isFetching,
    error: retrieval.error,
  }
}

export function RetrievalTest({
  workspaceSlug,
  kbId,
  search,
  defaults,
  activeGenerationLabel,
  useResults = useDefaultRetrievalResults,
}: RetrievalTestProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState(search.q ?? '')
  const [vectorTopK, setVectorTopK] = useState(
    search.vectorTopK ?? defaults.vector_top_k
  )
  const [keywordTopK, setKeywordTopK] = useState(
    search.keywordTopK ?? defaults.keyword_top_k
  )
  const [finalTopK, setFinalTopK] = useState(
    search.finalTopK ?? defaults.final_top_k
  )
  // activeRequest 是当前已提交的检索请求（驱动 AJAX 查询）。
  // 仅在点击「检索」后才更新，避免输入时频繁请求；初始值来自 URL 深链。
  const [activeRequest, setActiveRequest] = useState<RetrievalRequest | null>(
    search.q ? toRetrievalRequest(search, defaults) : null
  )
  // 记录最近一次请求开始时间，用于在完成后展示耗时。
  const [startedAt, setStartedAt] = useState<number | null>(null)
  // 上次完成请求的耗时（毫秒），用于在结果区展示。
  const [durationMs, setDurationMs] = useState<number | null>(null)
  const retrieval = useResults(workspaceSlug, kbId, activeRequest)
  const results = retrieval.data ?? []
  const isSearching = retrieval.isFetching
  const errorMessage = retrieval.error
    ? parseApiError(retrieval.error).message
    : undefined

  // 当请求从 fetching 变为完成时，冻结耗时。
  useTrackDuration(startedAt, isSearching, (elapsed) => setDurationMs(elapsed))

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const q = query.trim()
    if (!q) return
    setDurationMs(null)
    setStartedAt(Date.now())
    setActiveRequest({
      query: q,
      vector_top_k: vectorTopK,
      keyword_top_k: keywordTopK,
      final_top_k: finalTopK,
    })
  }

  return (
    <div className='space-y-5'>
      <form
        onSubmit={submit}
        className='space-y-3 rounded-xl border bg-card p-4'
      >
        <div className='flex flex-col gap-2 sm:flex-row'>
          <Input
            aria-label={t('retrieval.test.searchInputAriaLabel')}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('retrieval.test.searchInputPlaceholder')}
            className='h-10 flex-1'
          />
          <Button type='submit' disabled={!query.trim() || isSearching}>
            <Search />
            {isSearching
              ? t('retrieval.test.searchingButton')
              : t('retrieval.test.searchButton')}
          </Button>
        </div>
        <Collapsible>
          <CollapsibleTrigger asChild>
            <Button type='button' variant='ghost' size='sm'>
              {t('retrieval.test.advancedTrigger')}
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className='grid gap-3 pt-3 sm:grid-cols-3'>
            <div className='space-y-1.5'>
              <Label htmlFor='vector-top-k'>
                {t('retrieval.test.vectorTopKLabel')}
              </Label>
              <Input
                id='vector-top-k'
                type='number'
                min={1}
                value={vectorTopK}
                onChange={(event) => setVectorTopK(event.target.valueAsNumber)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='keyword-top-k'>
                {t('retrieval.test.keywordTopKLabel')}
              </Label>
              <Input
                id='keyword-top-k'
                type='number'
                min={1}
                value={keywordTopK}
                onChange={(event) => setKeywordTopK(event.target.valueAsNumber)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='final-top-k'>
                {t('retrieval.test.finalTopKLabel')}
              </Label>
              <Input
                id='final-top-k'
                type='number'
                min={1}
                max={50}
                value={finalTopK}
                onChange={(event) => setFinalTopK(event.target.valueAsNumber)}
              />
            </div>
          </CollapsibleContent>
        </Collapsible>
      </form>

      {errorMessage && (
        <p
          role='alert'
          className='rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-destructive text-sm'
        >
          {errorMessage}
        </p>
      )}

      {(activeRequest || isSearching) && (
        <div className='space-y-3'>
          {isSearching && (
            <div className='flex items-center gap-2 rounded-lg border bg-muted/30 p-3 text-muted-foreground text-sm'>
              <span className='size-4 animate-spin rounded-full border-2 border-current border-t-transparent' />
              {t('retrieval.test.searchingNotice')}
            </div>
          )}
          {!isSearching && (results.length > 0 || durationMs !== null) && (
            <div className='flex flex-col justify-between gap-2 sm:flex-row sm:items-end'>
              <div>
                <h2 className='font-semibold text-lg'>
                  {t('retrieval.test.evidenceTitle')}
                </h2>
                <p className='text-muted-foreground text-sm'>
                  {t('retrieval.test.evidenceCount', {
                    count: results.length,
                  })}
                  {activeGenerationLabel
                    ? t('retrieval.test.indexSuffix', {
                        label: activeGenerationLabel,
                      })
                    : ''}
                  {durationMs !== null
                    ? t('retrieval.test.durationSuffix', {
                        duration: formatDuration(durationMs),
                      })
                    : ''}
                </p>
              </div>
              <p className='max-w-xl text-muted-foreground text-xs'>
                {t('retrieval.test.scoreHint')}
              </p>
            </div>
          )}

          {results.map((result, index) => {
            const basePath = resultBasePath(workspaceSlug, kbId, result)
            const heading =
              typeof result.metadata.heading === 'string'
                ? result.metadata.heading
                : undefined
            const showHeading = heading && !result.content.includes(heading)
            return (
              <Card key={result.chunk_id}>
                <CardHeader>
                  <div className='flex min-w-0 items-start gap-3'>
                    <span className='flex size-7 shrink-0 items-center justify-center rounded-full bg-primary/10 font-semibold text-primary text-sm'>
                      {index + 1}
                    </span>
                    <div className='min-w-0 flex-1'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <CardTitle className='truncate'>
                          {result.document_name}
                        </CardTitle>
                        <Badge variant='outline'>{result.document_kind}</Badge>
                        <span className='ms-auto font-medium text-sm'>
                          {result.ranking_stage === 'rerank'
                            ? t('retrieval.test.rerankScore', {
                                value: score(result.rerank_score),
                              })
                            : t('retrieval.test.rrfScore', {
                                value: score(result.score),
                              })}
                        </span>
                      </div>
                      {result.ranking_stage === 'rrf_fallback' && (
                        <p
                          role='alert'
                          className='mt-2 rounded-md border border-amber-300 bg-amber-50 p-2 text-amber-900 text-xs dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200'
                        >
                          {t('retrieval.test.fallbackWarning')}
                        </p>
                      )}
                      <CardDescription className='mt-1'>
                        {showHeading && <span>{heading} · </span>}
                        {sourceAnchorLabel(result.source_anchor)}
                      </CardDescription>
                    </div>
                  </div>
                </CardHeader>
                <CardContent className='space-y-3'>
                  <div className='space-y-2'>
                    <p className='font-medium text-sm'>
                      {t('retrieval.test.fullContext')}
                    </p>
                    <SafeMarkdown content={result.content} />
                  </div>
                  <div className='space-y-2 rounded-lg bg-muted/40 p-3'>
                    <p className='font-medium text-sm'>
                      {t('retrieval.test.matchedChildren', {
                        count: result.matched_children.length,
                      })}
                    </p>
                    <ul className='space-y-2'>
                      {result.matched_children.map((child) => (
                        <li
                          key={child.chunk_id}
                          className='flex flex-wrap items-center gap-x-2 gap-y-1 text-muted-foreground text-xs'
                        >
                          <span className='min-w-0 flex-1 truncate text-foreground'>
                            {child.content.replace(/\s+/g, ' ').trim()}
                          </span>
                          <span>{sourceAnchorLabel(child.source_anchor)}</span>
                          <span>RRF {score(child.score)}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div className='flex flex-wrap items-center gap-x-4 gap-y-2 text-muted-foreground text-xs'>
                    {result.ranking_stage === 'rerank' && (
                      <span>
                        {t('retrieval.test.rrfScore', {
                          value: score(result.score),
                        })}
                      </span>
                    )}
                    <span>
                      {t('retrieval.test.vectorScore', {
                        value: score(result.vector_score),
                      })}
                    </span>
                    <span>
                      {t('retrieval.test.keywordScore', {
                        value: score(result.keyword_score),
                      })}
                    </span>
                    <span className='ms-auto flex gap-2'>
                      <Button asChild variant='outline' size='sm'>
                        <a href={basePath}>
                          {t('retrieval.test.viewSourceLink')}
                        </a>
                      </Button>
                      <Button asChild variant='outline' size='sm'>
                        <a
                          href={`${basePath}?chunk=${encodeURIComponent(result.matched_children[0]?.chunk_id ?? result.chunk_id)}&anchor=1`}
                        >
                          {t('retrieval.test.openChunkLink')}
                        </a>
                      </Button>
                    </span>
                  </div>
                </CardContent>
              </Card>
            )
          })}

          {activeRequest?.query &&
            results.length === 0 &&
            !isSearching &&
            !errorMessage &&
            durationMs !== null && (
              <div className='rounded-xl border border-dashed p-8 text-center'>
                <p className='font-medium'>{t('retrieval.test.emptyTitle')}</p>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t('retrieval.test.emptyDescription')}
                </p>
              </div>
            )}
        </div>
      )}
    </div>
  )
}
