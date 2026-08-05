import type { knowledgeBases as zhKnowledgeBases } from '../zh/knowledgeBases'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const knowledgeBases = {
  list: {
    eyebrow: 'Knowledge Processing',
    title: 'Knowledge Bases',
    subtitle:
      'Each knowledge base is explicitly bound to an Embedding model and maintains its own chunking rules.',
    createButton: 'Create Knowledge Base',
    emptyTitle: 'No knowledge bases yet',
    emptyDescription:
      'Upload and process documents after creating a knowledge base.',
    createFirstButton: 'Create your first knowledge base',
    noDescription: 'No description',
    viewAriaLabel: 'View {{name}}',
    modelUnavailable: 'Unavailable',
    embeddingMeta:
      '{{provider}} · {{dimensions}} dims · chunks {{chunkSize}} / overlap {{chunkOverlap}}',
  },

  form: {
    nameLabel: 'Name',
    descriptionLabel: 'Description',
    strategyLabel: 'Strategy',
    strategyOptions: {
      auto: 'Automatic',
      heading: 'By headings',
      heuristic: 'By document structure',
      recursive: 'Recursive',
    },
    parentChildLabel: 'Parent-child chunking',
    childSizeLabel: 'Child chunk size (for retrieval)',
    parentSizeLabel: 'Context chunk size (for return)',
    parentOverlapLabel: 'Parent chunk overlap',
    chunkSizeLabel: 'Chunk size',
    chunkOverlapLabel: 'Overlap size',
    createdToast: 'Knowledge base created',
    submitButton: 'Create Knowledge Base',
  },

  embeddingModelSelect: {
    label: 'Embedding Model',
    placeholder: 'Select a model',
    workspaceGroup: 'Current Workspace',
    platformGroup: 'Platform shared',
    noModelsManageable: 'No Embedding models are available.',
    configureModelsLink: 'Configure models first',
    noModelsMember:
      'Please contact a Workspace administrator to configure models.',
    option: '{{name}} · {{provider}} · {{dimensions}} dims',
  },

  overview: {
    countTotal: 'All content',
    countReady: 'Searchable',
    countProcessing: 'Processing',
    countFailed: 'Failed',
    activeIndexTitle: 'Current index version',
    activeModelLine: '{{modelName}} · {{dimensions}} dims',
    syncLabel: 'Index sync',
    contentVersion: 'Content version {{version}}',
    indexedVersion: 'Indexed {{version}}',
    scaleLabel: 'Index scale',
    documentScale: '{{documentCount}} documents · {{chunkCount}} chunks',
    missingActiveTitle: 'Missing current index version',
    missingActiveDescription:
      'This is not a normal empty state. Please check the knowledge base configuration.',
    blockersTitle: 'Needs attention',
    noBlockersTitle: 'No blockers',
    noBlockersDescription: 'Content and index status are healthy.',
    recentJobsTitle: 'Recent jobs',
    recentJobsDescription: 'Real backend activity; raw payloads are not shown.',
    noRecentJobs: 'No recent jobs',
    addContentButton: 'Add Content',
    searchTestButton: 'Test Search',
    buildIndexButton: 'Build New Index Version',
    checkCandidateButton: 'Review Candidate Version',
    jobStatus: {
      pending: 'Pending',
      queued: 'Queued',
      running: 'Running',
      completed: 'Completed',
      succeeded: 'Completed',
      failed: 'Failed',
      cancelled: 'Cancelled',
    },
  },

  settings: {
    nameRequired: 'Please enter a knowledge base name',
    basicsTitle: 'Basic Information',
    basicsDescription:
      'The name and description are for display only and do not change the indexed content.',
    readOnlyTitle: 'Read-only settings',
    readOnlyDescription:
      'Knowledge base basics are maintained by the Workspace administrator',
    nameLabel: 'Knowledge base name',
    descriptionLabel: 'Knowledge base description',
    saveBasicsButton: 'Save Basic Information',
    configTitle: 'Current Retrieval Configuration',
    configDescription:
      'Changing the model, chunking or retrieval parameters requires building a new index version.',
    modelLabel: 'Embedding Model',
    dimensionLabel: 'Dimensions',
    chunkingLabel: 'Chunking',
    retrievalLabel: 'Candidate / Final',
    noActiveVersion: 'No index version is currently active.',
    buildIndexButton: 'Build New Index Version',
    diagnosticsTitle: 'Diagnostics',
    diagnosticsDescription:
      'Internal identifiers are not shown on the page; copy them only when troubleshooting.',
    copyDiagnostics: 'Copy Diagnostics',
    copiedDiagnostics: 'Diagnostics copied',
  },

  workbench: {
    unnamedName: 'Untitled knowledge base',
    noDescription: 'No description',
    contentMeta: 'Content version {{version}} · {{chunkCount}} chunks',
    addContentButton: 'Add Content',
    areaAriaLabel: 'Knowledge base area',
    tabs: {
      overview: 'Overview',
      content: 'Content',
      search: 'Search Test',
      indexes: 'Indexes',
      settings: 'Settings',
    },
    syncState: {
      synced: 'Synced',
      updating: 'Updating',
      failed: 'Has failures',
      candidate_ready: 'Candidate pending activation',
    },
  },

  schemas: {
    nameRequired: 'Please enter a name',
    embeddingModelRequired: 'Please select an Embedding model',
    chunkSizeInvalid: 'Please enter a chunk size',
    chunkSizeInteger: 'Chunk size must be an integer',
    chunkSizePositive: 'Chunk size must be greater than 0',
    chunkOverlapInvalid: 'Please enter an overlap size',
    chunkOverlapInteger: 'Overlap size must be an integer',
    chunkOverlapMin: 'Overlap size cannot be less than 0',
    chunkOverlapLessThanSize: 'Overlap size must be less than chunk size',
    childLargerThanParent:
      'Child chunk size cannot be larger than context chunk size',
  },
} satisfies Widen<typeof zhKnowledgeBases>
