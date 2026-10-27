/**
 * English resource for web content sources (v1.3.0 spec §9.4/§9.5).
 * Structure must mirror locales/zh/webSource.ts.
 */
export const webSource = {
  list: {
    addButton: 'Add website source',
    emptyTitle: 'No website sources yet',
    emptyDescription:
      'Once added, crawled pages are searched together with files uploaded to this knowledge base.',
    emptyAddButton: 'Add website source',
    columnName: 'Name',
    columnSummary: 'Summary',
    columnStatus: 'Status',
    columnPages: 'Pages',
    columnLastSync: 'Last sync',
    columnActions: 'Actions',
    enableButton: 'Enable',
    enterAriaLabel: 'Open {{name}}',
  },

  card: {
    seedLabel: 'seed',
    sitemapLabel: 'sitemap',
    urlCount: '{{count}} URLs',
    pageCount: '{{count}} pages',
    pageCountWithFailed: '{{count}} pages ({{failed}} failed)',
    syncNow: 'Sync now',
    enable: 'Enable',
    enter: 'Open',
    syncQueuedToast: 'Sync queued',
    lastSyncNone: 'Never synced',
    lastSyncAt: 'Last {{time}}',
    lastSyncSynced: '{{count}} synced',
    lastSyncDeleted: '{{count}} deleted',
    lastSyncFailed: '{{count}} failed',
    lastSyncFailedOnly: 'Last sync failed',
    nextSyncAt: 'Next sync {{time}}',
    nextSyncPaused: 'Scheduling paused',
  },

  health: {
    ok: 'Healthy',
    partial: 'Partial failure',
    failed: 'Failed',
    disabled: 'Disabled',
  },

  mode: {
    crawl: 'Crawl',
    sitemap: 'Sitemap',
    urls: 'URL list',
  },

  engine: {
    http: 'HTTP',
    auto: 'Auto',
    render: 'Render browser',
    firecrawl: 'Firecrawl',
    jina: 'Jina Reader',
  },

  detail: {
    pagesTitle: 'Pages captured by this source {{count}}',
    respectRobotsOn: 'Respects robots',
    respectRobotsOff: 'Ignores robots',
    cronOn: 'Scheduled {{cron}}',
    cronOff: 'Manual sync only',
    onDeleteKeep: 'Keep pages when they disappear',
    onDeleteRemove: 'Remove pages when they disappear',
    syncNow: 'Sync now',
    editButton: 'Edit config',
    manageUrlsButton: 'Manage URLs',
    disableButton: 'Disable',
    enableButton: 'Enable',
    deleteButton: 'Delete',
    syncQueuedToast: 'Sync queued',
    statusChangedToast: 'Status updated',
  },

  // Force re-crawl confirmation (v1.4.0 §9.3.7): shared by the list card and source detail.
  forceRecrawl: {
    button: 'Force re-crawl',
    title: 'Force re-crawl',
    description:
      'This will ignore change detection, re-crawl all {{pages}} pages, and generate new versions. It may take a long time and consume your sync quota. Use it only when upstream change detection appears broken.',
    cancel: 'Cancel',
    confirm: 'Start force re-crawl',
  },

  form: {
    createTitle: 'Add website source',
    createDescription:
      'Crawled pages are added to this knowledge base as documents and searched together with uploaded files.',
    editTitle: 'Edit "{{name}}"',
    editDescription: 'Saving triggers a sync for this source.',
    nameLabel: 'Name',
    namePlaceholder: 'e.g. Product docs',
    modeLabel: 'Import mode',
    modeCrawlDesc: 'BFS discovery limited to the seed URL',
    modeSitemapDesc: 'Automatic discovery via sitemap',
    modeUrlsDesc: 'Manually maintained; edits sync immediately',
    seedUrlLabel: 'Seed URL',
    seedUrlHint: 'Crawling starts here; immutable after creation.',
    sitemapUrlLabel: 'Sitemap URL',
    sitemapUrlHint: 'Sitemap entry point; immutable after creation.',
    urlsLabel: 'URL list',
    urlsPlaceholder: 'https://example.com/page',
    urlsHint:
      'One URL per line; formats are checked per line before saving, normalization is handled by the system.',
    urlsEditHint:
      'Maintain the page list via "Manage URLs" on the source detail page.',
    advancedTitle: 'Advanced options',
    advancedSummary:
      'Limits · path filters · fetch engine · extraction · schedule · delete policy',
    maxPagesLabel: 'Max pages',
    maxDepthLabel: 'Max depth',
    includePathsLabel: 'Include paths',
    includePathsPlaceholder: 'e.g. /docs/*, /blog/**',
    excludePathsLabel: 'Exclude paths',
    excludePathsPlaceholder: 'e.g. /tags/*',
    pathsHint:
      'Globs with * and ** allowed, separated by commas or spaces; exclude wins.',
    allowSubdomainsLabel: 'Allow subdomains',
    respectRobotsLabel: 'Respect robots.txt',
    requestDelayLabel: 'Request delay (ms)',
    requestDelayPlaceholder: 'Leave empty for system default',
    engineLabel: 'Fetch engine',
    engineHint: 'Immutable after creation.',
    connectionLabel: 'Fetch connection',
    connectionHint:
      'engine=http needs no connection; other engines require one.',
    connectionEmpty:
      'No connection available. Create one on the integrations page first.',
    connectionGo: 'Open integrations',
    extractModeLabel: 'Content extraction',
    extractReadability: 'Smart extraction (readability)',
    extractFull: 'Full page as Markdown',
    fallbackToFullLabel: 'Fall back to full page when extraction fails',
    syncScheduleLabel: 'Scheduled sync',
    syncScheduleHint: 'Sync this source on a cron schedule.',
    cronLabel: 'Cron expression',
    cronHint: '5-field cron (min hour dom month dow), e.g. 0 */6 * * *.',
    onDeleteLabel: 'When pages disappear',
    onDeleteKeep: 'Keep pages',
    onDeleteRemove: 'Remove on sync',
    cancel: 'Cancel',
    submitCreate: 'Add and queue sync',
    submitEdit: 'Save',
    createdToast: 'Website source created, sync queued',
    savedToast: 'Config saved, sync queued',
  },

  manageUrls: {
    title: 'Manage URLs · {{name}}',
    description:
      '{{count}} URLs currently. Uncheck to remove, paste new lines to add; saving triggers a sync.',
    searchPlaceholder: 'Search URLs…',
    searchAriaLabel: 'Search URLs',
    selectAll: 'Select all',
    noMatches: 'No matching URLs',
    currentLabel: 'Current URLs',
    addLabel: 'Add (one per line)',
    addPlaceholder: 'https://example.com/new-page',
    previewAdded: '{{count}} added',
    previewRemoved: '{{count}} removed',
    previewDuplicate:
      '{{count}} lines already in the list (ignored on save): {{samples}}',
    previewInvalid: '{{count}} invalid lines will be rejected: {{samples}}',
    save: 'Save',
    cancel: 'Cancel',
    savedToast: 'URL list saved, sync queued',
    removePolicyKeep:
      'Pages of removed URLs stay in the knowledge base (policy: keep pages).',
    removePolicyRemove:
      'Pages of removed URLs are removed on sync (policy: remove on sync).',
  },

  deleteDialog: {
    title: 'Delete "{{name}}"?',
    description:
      'Deleting the source stops its scheduled sync; the source itself cannot be restored.',
    pagesLabel: 'Page handling',
    pagesKeepLabel: 'Remove source only (keep pages)',
    pagesKeepDescription:
      'Captured pages remain in the knowledge base and stay searchable.',
    pagesRemoveLabel: 'Delete pages as well',
    pagesRemoveDescription:
      'The {{count}} pages captured by this source will be removed from the knowledge base.',
    cancel: 'Cancel',
    confirm: 'Delete',
    deletedToast: 'Source deleted',
  },

  schema: {
    nameRequired: 'Name is required',
    seedUrlRequired: 'Seed URL is required',
    sitemapUrlRequired: 'Sitemap URL is required',
    urlsRequired: 'Enter at least one valid URL',
    invalidUrl: 'Enter a valid http(s) URL',
    positiveIntRequired: 'Must be a positive integer',
    nonNegativeIntRequired: 'Must be a non-negative integer',
    connectionRequired: 'This engine requires a fetch connection',
    cronRequired: 'Cron is required when scheduled sync is enabled',
    cronFormat: 'Cron must have 5 fields (min hour dom month dow)',
  },
} as const
