import type { contentFaq as zhContentFaq } from '../zh/contentFaq'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const contentFaq = {
  // FAQ form (FAQForm)
  form: {
    conflictTitle: 'The FAQ has been updated by someone else',
    saveFailedTitle: 'Failed to save the FAQ',
    conflictDescription:
      'Your input is still kept in the form. Compare it with the latest version, merge manually, then retry based on the latest version.',
    retryOnLatest: 'Retry with the latest version',
    titleLabel: 'Title',
    titlePlaceholder: 'e.g. Refund policy',
    titleDescription:
      'The title is used as a readable name in the content list and search results.',
    questionsTitle: 'Question variants',
    questionsHint:
      'Different phrasings of the same intent share this one answer. Use Alt + ↑/↓ to reorder.',
    addQuestion: 'Add question',
    questionLabel: 'Question {{index}}',
    moveQuestionUpAriaLabel: 'Move question {{index}} up',
    moveQuestionDownAriaLabel: 'Move question {{index}} down',
    deleteQuestionAriaLabel: 'Delete question {{index}}',
    questionPlaceholder: 'Enter a question users might ask',
    answerLabel: 'Answer',
    answerViewAriaLabel: 'Answer view',
    viewEdit: 'Edit',
    viewPreview: 'Preview',
    viewSplit: 'Split',
    answerPlaceholder: 'Write a complete, reusable answer in Markdown…',
    previewEmptyHint: 'Enter an answer to preview it here.',
    answerDescription:
      'Markdown is rendered after a safety filter; HTML is not executed directly.',
    saveCreate: 'Save FAQ',
    saveNewVersion: 'Save new version',
  },

  // FAQ detail (FAQDetail)
  detail: {
    statusPending: 'Pending',
    statusProcessing: 'Processing',
    statusReady: 'Ready',
    statusFailed: 'Failed',
    statusDeleting: 'Deleting',
    statusDeleted: 'Deleted',
    untitled: 'Untitled FAQ',
    revisionLabel: 'Revision {{no}}',
    editButton: 'Edit FAQ',
    indexingTitle: 'The new version is being indexed',
    indexingDescription:
      'This page refreshes the status automatically; until indexing completes, search still uses the previous active version.',
    failedTitle: 'FAQ processing failed',
    failedFallbackMessage: 'Check the content and save a new version.',
    questionsTitle: 'Question variants',
    questionsCountDescription: '{{count}} phrasings share the same answer',
    answerTitle: 'Answer',
    answerDescription: 'Markdown rendered with the safety rules applied',
  },

  // Revision conflict comparison (FAQConflictComparison)
  conflictComparison: {
    yourVersion: 'Your version',
    latestVersion: 'Latest version',
    questionsLabel: 'Question variants',
    answerLabel: 'Answer',
  },

  // Form validation (faqFormSchema)
  schema: {
    titleRequired: 'Please enter an FAQ title',
    questionRequired: 'Question cannot be empty',
    questionsMin: 'Add at least one question',
    answerRequired: 'Please enter an answer',
    questionsDuplicate: 'Questions cannot be duplicated',
  },
} satisfies Widen<typeof zhContentFaq>
