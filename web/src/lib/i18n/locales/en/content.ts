import type { content as zhContent } from '../zh/content'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const content = {
  // File tree (FileTree)
  fileTree: {
    newFolderAriaLabel: 'New folder',
    searchAriaLabel: 'Search folders',
    searchPlaceholder: 'Search folders…',
    treeAriaLabel: 'Folders',
    renameAction: 'Rename',
    moveAction: 'Move',
    deleteAction: 'Delete',
    renameActionAriaLabel: 'Rename {{name}}',
    moveActionAriaLabel: 'Move {{name}}',
    deleteActionAriaLabel: 'Delete {{name}}',
    rowActionsAriaLabel: 'Actions for {{name}}',
    noMatches: 'No matching folders',
    empty: 'No folders yet',
    createFolder: {
      modalTitle: 'New folder',
      formAriaLabel: 'Create folder in {{path}}',
      nameAriaLabel: 'Folder name',
      namePlaceholder: 'e.g. guides',
      cancel: 'Cancel',
      create: 'Create',
    },
    rename: {
      modalTitle: 'Rename',
      inputAriaLabel: 'Rename {{name}}',
      nameRequired: 'Name cannot be empty',
      nameTooLong: 'Name cannot exceed 255 characters',
      cancel: 'Cancel',
      save: 'Save name',
    },
    move: {
      dialogAriaLabel: 'Choose a target folder',
      title: 'Choose a target folder',
      modalTitle: 'Move “{{name}}”',
      currentLocation: 'Current location: {{path}}',
      toTarget: 'Move to {{path}}',
      confirm: 'Move to {{path}}',
      cancel: 'Cancel',
    },
    delete: {
      dialogAriaLabel: 'Confirm delete {{name}}',
      title: 'Delete “{{name}}”?',
      modalTitle: 'Confirm delete',
      filePath: '{{path}}',
      fileWarning:
        'File deletion is irreversible; associated chunks and vectors will be removed.',
      folderWarning:
        'A non-empty folder cannot be deleted; clear its contents first.',
      fileDescription:
        'The file will be removed from retrieval; the backend performs a soft delete.',
      folderDescription:
        'A non-empty folder cannot be deleted; move or delete its contents first.',
      cancel: 'Cancel',
      confirm: 'Confirm delete',
      deleteAction: 'Delete',
      // Citation warning (v1.4.0 §9.3.5): rendered below the node info block when citation_count > 0.
      citationWarningTitle: 'Recently cited in retrieval',
      citationWarningBody:
        'This content was returned in {{count}} searches in the last 30 days{{suffix}}. Related questions will lose their evidence after deletion; make sure it is no longer needed.',
      citationLastCited: ', most recently {{time}}',
    },
    errors: {
      notEmpty: 'The folder still has contents; move or delete them first.',
      nameConflict:
        'An item with the same name already exists in the target folder; choose a different name.',
      cycle: 'A folder cannot be moved into itself or its own subfolders.',
    },
    schema: {
      fileRequiresDocument: 'File nodes must have a document',
      folderCannotHaveDocument: 'Folder nodes cannot have a document',
    },
  },

  // File tree workspace (FileTreeWorkspace)
  fileWorkspace: {
    uploadFile: 'Upload file',
  },

  fileBrowser: {
    label: 'Files in current folder',
    currentFolder: 'Current folder: {{path}}',
    uploadFile: 'Upload file',
    createFolder: 'New folder',
    searchAriaLabel: 'Search current folder files',
    searchPlaceholder: 'Search current folder files…',
    statusAriaLabel: 'Filter by status',
    sortAriaLabel: 'File order',
    allStatuses: 'All statuses',
    sortUpdated: 'Recently updated',
    sortName: 'Name',
    emptyTitle: 'This folder has no files',
    emptyDescription:
      'Upload a file and it will be processed and indexed in the background.',
    columnName: 'Name',
    columnStatus: 'Status',
    columnUpdatedAt: 'Updated',
    columnActions: 'Actions',
    rowActionsAriaLabel: 'Actions for {{name}}',
    viewFile: 'View content',
  },

  fileUpload: {
    modalTitle: 'Upload file',
    description:
      'The file will be uploaded to “{{path}}” and processed in the background.',
  },

  // Content list (ContentList)
  contentList: {
    kindFile: 'File',
    kindFaq: 'FAQ',
    kindWeb: 'Web',
    statusPending: 'Pending',
    statusProcessing: 'Processing',
    statusReady: 'Ready',
    statusFailed: 'Failed',
    statusDeleting: 'Deleting',
    statusDeleted: 'Deleted',
    allLabel: 'All content',
    unnamed: 'Untitled document',
    noResultsTitle: 'No content matches the criteria',
    noResultsHint: 'Adjust the search or filter criteria and try again.',
    columnName: 'Name',
    columnType: 'Type',
    columnSummary: 'Summary',
    columnStatus: 'Status',
    columnUpdatedAt: 'Updated',
    columnActions: 'Actions',
    viewAriaLabel: 'View {{name}}',
    faqCount: '{{count}} questions',
    noSourceUri: 'No source URL recorded',
    fallbackType: 'File',
    updatedOn: 'Updated {{date}}',
  },

  // Document preview (DocumentPreview)
  documentPreview: {
    unnamed: 'Untitled document',
    tabListAriaLabel: 'Document view',
    tabPreview: 'Preview',
    tabRaw: 'Raw Markdown',
    tabInfo: 'File info',
    tabChunks: 'Chunks {{count}}',
    noNormalizedContent: 'No normalized content available for preview yet.',
    noRawMarkdown: 'No raw Markdown available yet.',
    fieldOriginalFilename: 'Original file name',
    fieldFileType: 'File type',
    fieldMime: 'MIME',
    fieldSize: 'Size',
    fieldRevision: 'Revision',
    fieldUpdatedAt: 'Updated',
    fieldSource: 'Source',
    fieldSourceUri: 'Source URL',
    unknown: 'Unknown',
    revisionNo: 'Revision {{no}}',
    noRevision: 'No revision available',
    assetsTitle: 'Image assets ({{count}})',
    warningsTitle: 'Parse warnings ({{count}})',
    noAssets: 'No image assets for this document',
  },

  // Content layout (ContentLayout)
  contentLayout: {
    tabsAriaLabel: 'Content type',
    tabAll: 'All content',
    tabFiles: 'Files',
    tabFaq: 'FAQ',
    tabWeb: 'Web',
    webTabCount: '{{count}} sources',
  },
} satisfies Widen<typeof zhContent>
