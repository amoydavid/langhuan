/**
 * English resource for the Health insights tab (v1.4.0 spec §9).
 * Structure must mirror locales/zh/insights.ts.
 */
export const insights = {
  breadcrumb: 'Health',

  page: {
    scopeNote: 'Scope: excludes console search tests',
    windowAriaLabel: 'Time range',
    window7: 'Last 7 days',
    window14: 'Last 14 days',
    window30: 'Last 30 days',
  },

  empty: {
    title: 'No search data yet',
    description:
      'Once searches run via MCP, REST, or the console test page, knowledge base health will appear here.',
  },

  member: {
    title: 'Health insights are visible to workspace admins only',
    description:
      'Empty-recall rate, sleeping content, and the activity feed are for knowledge base maintainers. Contact a workspace admin or the owner to view them.',
  },

  cards: {
    runsTitle: 'Searches (last {{window}} days)',
    runsSub: 'Empty {{empty}} · Degraded {{degraded}}',
    emptyRateTitle: 'Empty-recall rate',
    emptyRatePrevious: 'Previous {{rate}}',
    emptyRateNoPrevious: 'No previous window',
    deltaDown: '{{delta}}pp vs previous',
    deltaUp: '+{{delta}}pp vs previous',
    sleepingTitle: 'Sleeping content',
    sleepingSub: 'Zero citations in 30 days',
    sleepingInsufficient: 'Insufficient sample',
    changesTitle: 'Changes (last {{window}} days)',
    changesSub: 'New versions and removals',
  },

  zeroRecall: {
    title: 'Zero-recall queries',
    description:
      'Queries that returned nothing in the last {{window}} days (grouped by query)',
    columnQuery: 'Query',
    columnMasked: 'Query digest (masked)',
    columnHits: 'Count',
    columnFirstSeen: 'First seen',
    columnLastSeen: 'Last seen',
    columnActions: 'Actions',
    maskedQuery: '••• ({{chars}} chars)',
    createFaq: 'Create FAQ',
    testSearch: 'Test search',
    disabledTitle: 'Query text logging is off',
    disabledDescription:
      'Only counts and trends are available right now; enabling logging lets you turn zero-recall queries directly into FAQs.',
    enableButton: 'Enable logging',
    emptyTitle: 'No zero-recall queries in the last {{window}} days',
    emptyHint:
      'The knowledge base returned content for every recent query. That is a good state.',
    enableDialog: {
      title: 'Enable query text logging',
      description:
        'Once enabled, raw query text for this knowledge base is retained for 30 days, readable by workspace admins only, and can be turned off at any time. Historical text expires with the retention window.',
      cancel: 'Cancel',
      confirm: 'Enable logging',
      successToast: 'Query text logging enabled',
    },
  },

  sleeping: {
    title: 'Sleeping content',
    description:
      'Not returned by any search in 30 days · indexed more than 30 days ago',
    columnName: 'Name',
    columnKind: 'Type',
    columnStatus: 'Status',
    columnIndexdAt: 'Updated',
    columnActions: 'Actions',
    statusReady: 'Searchable',
    viewAriaLabel: 'View {{name}}',
    insufficientTitle: 'Insufficient search sample',
    insufficientDescription:
      'Fewer than 10 searches completed in the last 30 days, so sleeping content cannot be determined yet.',
    emptyTitle: 'No sleeping content',
    emptyHint: 'Searches in the last 30 days covered every indexed document.',
    kind: {
      file: 'File',
      web: 'Web',
      faq: 'FAQ',
    },
  },

  activity: {
    title: 'Content activity',
    description: 'All sources · latest {{count}} events',
    badgeSync: 'Sync',
    badgeRevision: 'Change',
    badgeDelete: 'Removed',
    syncSuccess: '{{name}} synced',
    syncFailed: '{{name}} sync failed',
    revisionReason: {
      ingest: 'uploaded a new version',
      replace: 'uploaded a new version',
      crawl: 'recrawled',
      edit: 'edited content',
      reparse: 'reparsed',
      fallback: 'updated content',
    },
    revisionLine: '{{title}} {{reason}}',
    deleteLine: '{{title}} removed from search',
    empty: 'No recent content changes',
  },
} as const
