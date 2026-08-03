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
    editButton: 'Edit chunk',
    chunkListAriaLabel: 'Chunk list',
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
  },
} satisfies Widen<typeof zhChunks>
