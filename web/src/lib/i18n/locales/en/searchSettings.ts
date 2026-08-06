export const searchSettings = {
  form: {
    enabledLabel: 'Enable global Rerank',
    enabledDescription:
      'Rerank merged candidates from all selected KnowledgeBases with one model.',
    modelLabel: 'Rerank model',
    modelPlaceholder: 'Select a Rerank model',
    modelDescription:
      'Only active Rerank models visible to this Workspace are listed.',
    candidateLabel: 'Candidate count',
    candidateDescription:
      'Number of candidates sent to Rerank, from 50 to 200.',
    failureLabel: 'Failure strategy',
    failureFallback: 'Fallback to RRF',
    failureFail: 'Return an error',
    failureDescription:
      'What to do when the remote Rerank service is temporarily unavailable.',
    scopeDescription:
      'This strategy applies to single- and multi-KnowledgeBase knowledge_search in this Workspace. Only Workspace administrators can change it.',
    save: 'Save strategy',
    saved: 'Search strategy saved',
  },
} as const
