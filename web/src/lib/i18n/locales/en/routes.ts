import type { routes as zhRoutes } from '../zh/routes'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

/**
 * 路由页面（web/src/routes）与错误页（web/src/features/errors）的文案。
 */
export const routes = {
  // Auth-related pages ((auth)/)
  auth: {
    setup: {
      title: 'Initialize Langhuan',
      description:
        'Create the first platform administrator. Sign in with that account when done.',
    },
    invitation: {
      loading: 'Loading invitation…',
      invalid:
        'The invitation does not exist, has expired, or has already been used.',
      title: 'Join Langhuan',
      description:
        'You are invited to join "{{name}}". Set your nickname and password.',
    },
  },

  // Error pages ((errors)/ and features/errors)
  errors: {
    back: 'Go back',
    home: 'Go home',
    forbidden: {
      title: 'Access denied',
      description:
        'Your account does not have permission to view this resource.',
    },
    unauthorized: {
      title: 'Sign in required',
      description:
        'Sign in with a valid account before accessing this resource.',
    },
    notFound: {
      title: 'Page not found',
      description:
        'The page you are looking for does not exist or has been removed.',
    },
    general: {
      title: 'The system is temporarily unable to complete the request',
      description:
        'Please try again later. If the problem persists, contact the administrator.',
    },
    maintenance: {
      title: 'Service under maintenance',
      description:
        'The console is currently unavailable. Please try again later.',
      reload: 'Reload',
    },
  },

  // Platform admin (_authenticated/admin)
  admin: {
    breadcrumb: 'Platform Admin',
    models: {
      breadcrumb: 'Platform Models',
      detail: { breadcrumb: 'Connection Details' },
    },
  },

  // Workspace pages (_authenticated/workspaces)
  workspaces: {
    breadcrumb: 'Workspaces',
    workspace: {
      breadcrumb: 'Workspace',
    },
    new: {
      breadcrumb: 'Create Workspace',
      eyebrow: 'Platform Admin',
      title: 'Create Workspace',
      description:
        'Workspaces isolate knowledge bases, documents, and member permissions.',
      cardTitle: 'Basic Information',
      cardDescription:
        'The slug appears in console URLs. Use a stable, readable value.',
    },
    overview: { breadcrumb: 'Overview' },
    members: {
      breadcrumb: 'Members',
      eyebrow: 'Workspace Permissions',
      title: 'Members',
      description: 'Roles determine what members can do in this workspace.',
    },
    invitations: {
      breadcrumb: 'Invitations',
      eyebrow: 'Workspace Permissions',
      title: 'Invitations',
      description:
        'Full invitation links are shown once when created. The history only keeps the token prefix.',
    },
    notFound: {
      title: 'Workspace not found or you do not have access',
      description:
        'Check the URL, or return to the workspace list to pick one you can access.',
    },
    documents: {
      detail: { breadcrumb: 'Document Details' },
    },
    jobs: {
      detail: { breadcrumb: 'Processing Jobs' },
    },
    models: {
      breadcrumb: 'Models',
      detail: { breadcrumb: 'Connection Details' },
    },
    searchSettings: {
      breadcrumb: 'Search settings',
      eyebrow: 'Workspace configuration',
      title: 'Search settings',
      description:
        'Configure the global Rerank strategy used by single- and multi-KnowledgeBase searches in this Workspace.',
    },
    apiKeys: {
      breadcrumb: 'API Key',
      new: { breadcrumb: 'Create API Key' },
      detail: {
        breadcrumb: 'Key Details',
        notFoundTitle: 'API Key not found or you do not have access',
        notFoundDescription:
          "Check the URL, or return to this workspace's API Key list.",
      },
    },

    // Integrations (integrations)
    integrations: {
      breadcrumb: 'Integrations',
      new: {
        breadcrumb: 'Add Feishu App',
        eyebrow: 'Workspace / Integrations',
        title: 'Add Feishu App',
        description:
          'Enter the app credentials from the Feishu developer console to use it in this workspace.',
        cardTitle: 'App Credentials',
        cardDescription:
          'The App Secret is only used to call Feishu APIs on the backend and is never echoed on the page.',
      },
    },

    // Knowledge bases (kb)
    kb: {
      breadcrumb: 'Knowledge Bases',
      new: {
        breadcrumb: 'Create Knowledge Base',
        eyebrow: 'Knowledge Processing',
        title: 'Create Knowledge Base',
        description:
          'Choose an available embedding model and configure document chunking rules.',
        cardTitle: 'Knowledge Base Configuration',
        cardDescription:
          'Default chunking is 512 / 80; adjust it to match your documents.',
      },
      detail: {
        breadcrumb: 'Knowledge Base',
        loading: 'Loading knowledge base workbench',
        notFoundTitle: 'Knowledge base not found or you do not have access',
        notFoundDescription:
          "Check the URL, or return to this workspace's knowledge base list.",
      },
      documents: {
        new: { breadcrumb: 'Upload Document' },
      },
      indexes: {
        breadcrumb: 'Indexes',
        buildStartedToast: 'Index version build started',
        activatedToast: 'Index version activated',
        reindexStartedToast:
          'Index rebuild started — activate it manually after the build completes',
        title: 'Index Versions',
        description:
          'While a new version builds, retrieval keeps using the currently active version.',
        buildButton: 'Build New Index Version',
        reindexButton: 'Rebuild Index',
        candidateTitle: 'Build Candidate Index Version',
      },
      settings: {
        breadcrumb: 'Settings',
        savedToast: 'Knowledge base basic info updated',
      },
      search: {
        breadcrumb: 'Retrieval Test',
        noIndexTitle: 'No index is currently available',
        noIndexDescription:
          'Add content and wait for the index to finish building before running a retrieval test.',
      },
      content: {
        breadcrumb: 'Content',
        all: {
          breadcrumb: 'All Content',
          searchPlaceholder: 'Search titles…',
          searchAriaLabel: 'Search content',
          statusAriaLabel: 'Content status',
          statusAll: 'All statuses',
          statusReady: 'Searchable',
          statusProcessing: 'Processing',
          statusFailed: 'Failed',
          statusDeleting: 'Deleting',
          statusDeleted: 'Deleted',
          sortAriaLabel: 'Sort content',
          sortUpdated: 'Recently updated',
          sortName: 'Name',
          uploadButton: 'Upload Files',
          newFaqButton: 'New FAQ',
        },
        faq: {
          breadcrumb: 'FAQ',
          searchAriaLabel: 'Search FAQ',
          searchPlaceholder: 'Search FAQ titles…',
          statusAriaLabel: 'FAQ status',
          statusAll: 'All statuses',
          statusReady: 'Searchable',
          statusProcessing: 'Processing',
          statusFailed: 'Failed',
          newButton: 'New FAQ',
          new: {
            breadcrumb: 'New FAQ',
            title: 'New FAQ',
            description:
              'Maintain a safely rendered Markdown answer with a set of user phrasings.',
            backToList: 'Back to FAQ list',
          },
          detail: {
            breadcrumb: 'FAQ Details',
            unnamedTitle: 'Untitled FAQ',
          },
          edit: {
            breadcrumb: 'Edit FAQ',
            title: 'Edit "{{title}}"',
            unnamedTitle: 'Untitled FAQ',
            description:
              'Saving creates an immutable revision. Retrieval keeps using the previous version until the index rebuilds.',
            backToDetail: 'Back to details',
            savedToast: 'New version is being indexed',
          },
        },
        files: {
          breadcrumb: 'Files',
          selectTitle: 'Select a file',
          selectDescription:
            'Open a file from the file tree on the left to view its normalized preview and the active chunks of the current index version.',
          upload: {
            breadcrumb: 'Upload Files',
            title: 'Upload Files',
            description:
              'After uploading you will land on the file\u2019s page; parsing and indexing run in the background.',
            cardTitle: 'Choose Files',
            cardDescription:
              'Supported files are saved to the currently selected directory.',
          },
          detail: {
            breadcrumb: 'File Details',
            unnamedTitle: 'Untitled File',
            confirmLeave: 'You have unsaved chunk edits. Leave anyway?',
            showOnlySearchable:
              'Show only chunks that participate in retrieval',
            viewJob: 'View processing job',
            viewChunks: 'View chunks ({{count}})',
            sheetTitle: 'Chunks of {{name}}',
            sheetDescription:
              'View the active chunks of the current index version, their original source, and revision history.',
            dialogTitle: 'Create Chunk Revision',
            dialogDescription:
              'Saving creates a new version, and the background job updates the current index.',
            chunkPanelLabel: 'Chunks',
            chunkDetailTitle: 'Chunk detail',
            chunkDetailDescription:
              'View the current content, original source, and revision history.',
          },
        },
        web: {
          breadcrumb: 'Web',
          searchAriaLabel: 'Search web content',
          searchPlaceholder: 'Search web titles…',
          statusAriaLabel: 'Web status',
          statusAll: 'All statuses',
          statusReady: 'Searchable',
          statusProcessing: 'Processing',
          statusFailed: 'Failed',
          detail: {
            breadcrumb: 'Web Details',
            unnamedTitle: 'Untitled Web Document',
          },
        },
      },
    },
  },
} satisfies Widen<typeof zhRoutes>
