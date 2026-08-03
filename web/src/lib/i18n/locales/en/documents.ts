import type { documents as zhDocuments } from '../zh/documents'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const documents = {
  uploadForm: {
    fileLabel: 'File',
    fileDescription:
      'Supports Markdown, TXT, CSV, XLSX, and DOCX; PDF is not supported yet.',
    fileRequired: 'Please select a file',
    titleLabel: 'Title',
    titleDescription:
      'Defaults to the file name after you select a file; you can change it.',
    titleRequired: 'Please enter a title',
    sourceTypeLabel: 'Source type',
    sourceTypeRequired: 'Please enter a source type',
    dedupeLabel: 'Reuse identical files',
    dedupeDescription:
      'Reuse an available document in this knowledge base with the same content hash.',
    submit: 'Upload and view processing status',
    dedupedToast: 'Reused an existing document',
    uploadedToast: 'Document uploaded, processing',
    uploadSizeLimitError:
      'The file exceeds the server size limit. Please choose a smaller file.',
    unsupportedTypeError:
      'This file type is not supported. Please choose a supported format.',
  },
  list: {
    title: 'Documents',
    description:
      'Uploaded documents are parsed, chunked, and indexed by background jobs.',
    uploadButton: 'Upload document',
    emptyTitle: 'No documents yet',
    emptyDescription:
      'Upload the first supported file to start building searchable content.',
    columnTitle: 'Document',
    columnStatus: 'Status',
    columnUpdatedAt: 'Updated',
    columnActions: 'Actions',
    viewAriaLabel: 'View {{title}}',
    status: {
      pending: 'Pending',
      processing: 'Processing',
      ready: 'Searchable',
      failed: 'Failed',
      deleting: 'Deleting',
      deleted: 'Deleted',
    },
  },
  detail: {
    eyebrow: 'Document details',
    viewJobButton: 'View processing job',
    failedTitle: 'Processing failed',
    fileInfoTitle: 'File information',
    fileTypeLabel: 'File type',
    fileSizeLabel: 'File size',
    documentTypeLabel: 'Document type',
    sourceTypeLabel: 'Source type',
    createdAtLabel: 'Created at',
    updatedAtLabel: 'Updated at',
    notApplicable: 'N/A',
    unknown: 'Unknown',
    advancedInfoTitle: 'Advanced information',
    notRecorded: 'Not recorded',
    notPublished: 'Not published yet',
  },
  job: {
    eyebrow: 'Processing job',
    failedTitle: 'Job failed',
    infoTitle: 'Job information',
    attemptsLabel: 'Attempts',
    createdAtLabel: 'Created at',
    updatedAtLabel: 'Updated at',
    notRecorded: 'Not recorded',
    status: {
      queued: 'Queued',
      running: 'Running',
      succeeded: 'Succeeded',
      failed: 'Failed',
      cancelled: 'Cancelled',
    },
  },
} satisfies Widen<typeof zhDocuments>
