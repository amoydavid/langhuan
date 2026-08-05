import type { chunks as zhChunks } from '../zh/chunks'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const chunks = {
  revisionForm: {
    noActiveRevision: 'This chunk has no editable active revision.',
    conflictTitle: 'Chunk was updated by someone else',
    saveFailedTitle: 'Failed to save revision',
    yourVersionTitle: 'Your version',
    latestVersionTitle: 'Latest version',
    retryWithLatest: 'Retry with the latest version',
    contextHeaderDescription: 'Add readable section context for this content.',
    contentLabel: 'Content',
    contentDescription:
      'Saving creates an immutable new revision and never overwrites the original source.',
    enabledLabel: 'Include in retrieval',
    enabledDescription:
      'When disabled, the history is kept but the chunk no longer appears in retrieval results.',
    cancelButton: 'Cancel',
    saveButton: 'Save new revision',
    schemas: {
      contextHeaderTooLong: 'Context header cannot exceed 500 characters',
      contentRequiredWhenEnabled:
        'Content cannot be empty when retrieval is enabled',
    },
  },
  inspector: {
    ariaLabel: 'Chunk inspector',
    emptyState: 'The current index version has no chunks to view.',
    title: 'Chunk {{sequence}} · {{title}}',
    parentGroup: 'Context chunk {{sequence}} · {{count}} child chunks',
    parentReadOnly:
      'Parent chunks provide full context only and cannot be retrieved or edited.',
    chunkTabNoTitle: 'Untitled',
    faqNotice:
      'Generated from FAQ content; edit the question and answer in the FAQ editor.',
    tabsAriaLabel: 'Chunk content views',
    tabCurrent: 'Current content',
    tabSource: 'Original source',
    tabHistory: 'Revision history',
    statusEnabled: 'Included in retrieval',
    statusDisabled: 'Disabled',
    statusUserEdited: 'Manually edited',
    noContextHeader: 'None',
    emptyContent: '(empty content)',
    revisionNo: 'Revision {{number}}',
    noActiveRevision: 'This chunk has no active revision.',
    historyEmpty: 'No revision history yet.',
    editSourceUser: 'Manually edited',
    editSourceSystem: 'System generated',
    revisionStatus: {
      pending: 'Waiting to index',
      indexing: 'Indexing',
      ready: 'Active',
      failed: 'Index failed',
    },
    anchor: {
      range: 'Lines {{start}}–{{end}}',
      rangeWithSheet: '{{sheet}} · lines {{start}}–{{end}}',
      unknown: 'Source position not recorded',
    },
    list: {
      ariaLabel: 'Chunk list',
      countTotal: '{{count}} chunks',
      countFiltered: '{{count}} chunks (filtered)',
      searchPlaceholder: 'Search context or content…',
      cardAriaLabel: 'View chunk {{sequence}}',
      cardEditAriaLabel: 'Edit chunk {{sequence}}',
      cardNoContent: '(no content preview)',
      cardMetaRevision: 'Revision {{number}}',
      cardMetaUpdated: 'Updated {{time}}',
      pageSizeLabel: '{{size}} / page',
      pageAriaLabel: 'Pagination',
      pagePrevious: 'Previous',
      pageNext: 'Next',
      pageCurrent: 'Page {{page}}',
    },
    detail: {
      ariaLabel: 'Chunk detail',
      editButton: 'Edit chunk',
      contentLabel: 'Context header',
    },
  },
} satisfies Widen<typeof zhChunks>
