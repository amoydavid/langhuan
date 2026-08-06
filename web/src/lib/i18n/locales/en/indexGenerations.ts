import type { indexGenerations as zhModule } from '../zh/indexGenerations'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const indexGenerations = {
  generationForm: {
    ariaLabel: 'Index configuration steps',
    hintAction: 'View help for {{label}}',
    steps: {
      embeddingModel: 'Embedding model',
      chunkConfig: 'Chunking',
      retrievalConfig: 'Retrieval',
    },
    stepHeadings: {
      model: '1. Embedding model',
      chunk: '2. Chunking',
      retrieval: '3. Retrieval',
    },
    modelStep: {
      description:
        'The candidate version locks in the model and dimensions without affecting the currently active version.',
      selectPlaceholder: 'Select a model',
      dimensions: '{{count}} dimensions',
      availableModelsDescription:
        'Only models available in the current Workspace are shown.',
    },
    chunkStep: {
      description:
        'Changing the chunking method may prevent existing manual edits from being migrated.',
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
      sizeLabel: 'Chunk size',
      sizeHint:
        'Characters per chunk. Larger chunks keep more context; smaller ones are more precise.',
      overlapLabel: 'Chunk overlap',
      overlapHint:
        'Characters shared between adjacent chunks, so key content is not cut off at boundaries.',
    },
    retrievalStep: {
      description:
        'Set the number of vector and keyword candidates and the final merged result count.',
      ftsLabel: 'Full-text search configuration',
      ftsHint:
        'Tokenization used for full-text keyword search. Choose "Chinese (zhparser)" for Chinese documents.',
      ftsSelectPlaceholder: 'Select a full-text search configuration',
      ftsOptions: {
        zhparser: 'Chinese (zhparser)',
        simple: 'Generic (simple)',
        english: 'English (english)',
        custom: 'Custom',
      },
      customFtsLabel: 'Custom full-text search configuration',
      customFtsPlaceholder: 'For example, german',
      customFtsDescription:
        'Enter the name of a text search configuration installed in the current PostgreSQL instance.',
      vectorTopKLabel: 'Vector candidates',
      vectorTopKHint:
        'Candidates retrieved by vector search. Higher is more complete but slower.',
      keywordTopKLabel: 'Keyword candidates',
      keywordTopKHint:
        'Candidates retrieved by keyword search. Higher is more complete but slower.',
      finalTopKLabel: 'Final results',
      finalTopKHint:
        'Number of results returned after merging vector and keyword candidates.',
      rrfKLabel: 'RRF K',
      rrfKHint:
        'Parameter for merging vector and keyword results; the default is usually fine.',
    },
    nextStepChunk: 'Next: Chunking',
    nextStepRetrieval: 'Next: Retrieval',
    previousStep: 'Previous',
    cancel: 'Cancel',
    submit: 'Build index version',
    validation: {
      selectEmbeddingModel: 'Please select an Embedding model',
      chunkSizePositive: 'Chunk size must be greater than 0',
      chunkOverlapNonNegative: 'Chunk overlap cannot be less than 0',
      selectFtsConfig: 'Please select a full-text search configuration',
      chunkOverlapLessThanSize: 'Chunk overlap must be less than chunk size',
      childLargerThanParent:
        'Child chunk size cannot be larger than context chunk size',
      selectRerankModel: 'Select a rerank model',
      candidateTopKRange: 'Candidate count must be between 50 and 200',
    },
  },
  generationList: {
    readOnlyAlert: {
      title: 'Read-only index history',
      description: 'Index configuration is managed by Workspace administrators',
    },
    empty: {
      title: 'No index versions yet',
      description:
        'After creating a knowledge base, the system keeps every index configuration and build result.',
    },
    status: {
      building: 'Building',
      ready: 'Ready',
      stale: 'Stale',
      failed: 'Build failed',
      retired: 'Retired',
    },
    activeBadge: 'Currently active',
    dimensions: '{{count}} dimensions',
    contentVersion: 'Content version {{version}}',
    contentSnapshot: 'Content snapshot {{version}}',
    indexedVersion: 'Indexed version {{version}}',
    compareActivate: 'Compare and activate',
    config: {
      chunk: 'Chunk {{size}}/{{overlap}}',
    },
    stats: {
      documents: '{{count}} documents',
      chunks: '{{count}} chunks',
      manualEdits: '{{count}} manual edits',
      disabledChunks: '{{count}} disabled',
    },
    rerankManagedByWorkspace: 'Rerank: controlled by Workspace search settings',
    dialog: {
      title: 'Compare and activate index version',
      description:
        'Once activated, new retrieval requests use the candidate version; historical versions remain available.',
      candidateModel: 'Candidate model',
      dimensionAndChunks: '{{dimension}} dimensions · {{chunks}} chunks',
      contentSnapshot: 'Content snapshot',
      snapshotVersion: 'Version {{version}}',
      snapshotIndexed: 'Indexed version {{version}}',
      archiveCount: '{{count}} manual edits will be archived',
      archiveDescription:
        'The chunking configuration has changed, so these edits cannot be migrated automatically to the candidate version.',
      archiveConfirmLabel: 'Confirm archiving manual edits',
      cancel: 'Cancel',
      confirmActivate: 'Confirm activation',
    },
  },
  indexWriteForbidden: {
    title: 'No permission to build index versions',
    description: 'Index configuration is managed by Workspace administrators',
    backButton: 'Back to index versions',
  },
} satisfies Widen<typeof zhModule>
