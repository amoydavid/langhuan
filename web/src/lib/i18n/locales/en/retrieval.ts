import type { retrieval as zhRetrieval } from '../zh/retrieval'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const retrieval = {
  test: {
    searchInputAriaLabel: 'Search question',
    searchInputPlaceholder: 'Enter the question to verify…',
    searchButton: 'Search',
    searchingButton: 'Searching…',
    advancedTrigger: 'Advanced parameters',
    vectorTopKLabel: 'Vector candidates',
    keywordTopKLabel: 'Keyword candidates',
    finalTopKLabel: 'Final results',
    searchingNotice: 'Searching…',
    evidenceTitle: 'Retrieval evidence',
    evidenceCount: 'Found {{count}} evidence items',
    indexSuffix: ' · current index "{{label}}"',
    durationSuffix: ' · took {{duration}}',
    scoreHint:
      'Scores only rank this result set; they are not relevance percentages or answer confidence.',
    fullContext: 'Full context returned',
    matchedChildren: 'Matched excerpts {{count}}',
    vectorScore: 'Vector {{value}}',
    keywordScore: 'Keyword {{value}}',
    viewSourceLink: 'View source',
    openChunkLink: 'Open chunk',
    emptyTitle: 'No matching evidence found',
    emptyDescription:
      'Try different keywords or increase the candidate counts.',
    anchor: {
      lineRange: 'Lines {{start}}–{{end}}',
      lineSingle: 'Line {{start}}',
      sheetLine: '{{sheet}} · {{line}}',
      paragraphRange: 'Paragraphs {{start}}–{{end}}',
      paragraphSingle: 'Paragraph {{start}}',
      unknown: 'Source position not provided',
    },
    durationMs: '{{ms}} ms',
    durationSec: '{{seconds}} s',
  },
} satisfies Widen<typeof zhRetrieval>
