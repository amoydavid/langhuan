import type { workspaceReadiness as zhModule } from '../zh/workspaceReadiness'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const workspaceReadiness = {
  recentKnowledgeBase: 'most recent knowledge base',
  relatedContent: 'related content',
  nextAction: {
    waitingAdmin: 'Waiting for administrator',
    provider: {
      eyebrow: 'Next · Model connection',
      title: 'Configure an available model connection',
      description:
        'Knowledge processing requires at least one active Provider visible in the current Workspace.',
      label: 'Configure model connection',
      memberTitle: 'Model connection is not ready',
      memberDescription:
        'Please ask a Workspace administrator to configure the model connection.',
    },
    embeddingModel: {
      eyebrow: 'Next · Embedding model',
      title: 'Create an Embedding model for knowledge bases',
      description:
        'Choose a supported vector dimension; once connected you can create knowledge bases.',
      label: 'Create Embedding model',
      memberTitle: 'Embedding model is not ready',
      memberDescription:
        'Please ask a Workspace administrator to create an available Embedding model.',
    },
    knowledgeBase: {
      eyebrow: 'Next · Knowledge base',
      title: 'Create your first knowledge base',
      description:
        'Choose a model and chunking configuration for a set of related content.',
      label: 'Create knowledge base',
    },
    addContent: {
      eyebrow: 'Next · {{kbName}}',
      title: 'Add content to "{{kbName}}"',
      description:
        'Upload files or create FAQs; content is processed and indexed in the background.',
      label: 'Add content',
    },
    waiting: {
      eyebrow: 'Processing · {{kbName}}',
      title: 'Waiting for "{{documentName}}" to finish processing',
      description:
        'The page shows only the real status returned by the backend; you can verify retrieval once processing finishes.',
      label: 'View processing status',
    },
    failed: {
      eyebrow: 'Action needed · {{kbName}}',
      description:
        'Content processing failed. Review the safe error summary, then re-import or delete the content.',
      label: 'Resolve failed content',
    },
    testRetrieval: {
      eyebrow: 'Next · {{kbName}}',
      title: 'Run a retrieval test for "{{kbName}}"',
      description:
        'Confirm the current index returns correct, traceable source evidence.',
      label: 'Start retrieval',
    },
    complete: {
      eyebrow: 'Ready',
      title: 'The Workspace is ready for knowledge retrieval',
      description:
        'You can continue adding content, verifying retrieval, or maintaining existing knowledge bases.',
    },
  },
  panel: {
    title: 'Readiness',
    description:
      'Computed from the real resource state of the current Workspace.',
  },
  rows: {
    provider: {
      label: 'Model connection',
      configured: 'Configured',
      notConfigured: 'Not configured',
    },
    embeddingModel: {
      label: 'Embedding model',
      available: 'Selectable models available',
      unavailable: 'No selectable models',
    },
    knowledgeBase: {
      label: 'Knowledge bases',
      count: '{{count}}',
    },
    content: {
      label: 'Content',
      detail:
        '{{ready}} searchable · {{processing}} processing · {{failed}} failed',
    },
    retrieval: {
      label: 'Retrieval verification',
      ready: '{{count}} knowledge bases ready',
      waiting: 'Waiting for searchable content',
    },
  },
  recent: {
    title: 'Recent knowledge bases',
    emptyTitle: 'No knowledge bases yet',
    emptyDescription: 'Once created, you can add files or FAQs.',
    unnamed: 'Unnamed knowledge base',
    noDescription: 'No description',
  },
  quickLinks: {
    title: 'Quick links',
    newKnowledgeBase: 'New knowledge base',
    models: 'Models',
    membersAndInvitations: 'Members & invitations',
    members: 'Members',
    searchTest: 'Retrieval test',
  },
} satisfies Widen<typeof zhModule>
