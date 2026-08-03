import type { apiKeys as zhApiKeys } from '../zh/apiKeys'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const apiKeys = {
  createPage: {
    eyebrow: 'Workspace Integrations',
    title: 'Create API Key',
    description:
      'Set a name, the accessible knowledge bases, and the permission scope. The full plaintext is shown once right after creation.',
    configTitle: 'Key Configuration',
    configDescription:
      'Keys expire after 90 days by default; you can set a fixed number of days or make the key non-expiring.',
  },
  listPage: {
    eyebrow: 'Workspace Integrations',
    title: 'API Keys',
    description:
      'Call Langhuan REST and MCP endpoints with an API Key; access is limited to the selected knowledge bases and scopes.',
    createButton: 'Create API Key',
    listTitle: 'API Keys',
    listDescription:
      'The plaintext secret is returned only once at creation; the list keeps only the prefix for identification.',
    loading: 'Loading…',
  },
  detailPage: {
    backToList: 'Back to list',
    eyebrow: 'Workspace Integrations',
    secretTitle: 'Secret',
    secretDescription:
      'The full plaintext is shown temporarily only on creation or when actively fetched and is never persisted; you can fetch it again at any time.',
    revokedNotice:
      'This Key is no longer valid; copying it will not restore access.',
    detailsTitle: 'Details',
    fieldName: 'Name',
    fieldPrefix: 'Prefix',
    fieldKnowledgeBases: 'Knowledge bases',
    noKnowledgeBases: 'None',
    fieldScopes: 'Permissions',
    fieldExpiry: 'Expiry',
    fieldLastUsed: 'Last used',
    neverUsed: 'Never used',
    fieldCreatedBy: 'Created by',
    unknownCreator: 'Unknown',
    fieldCreatedAt: 'Created at',
    fieldRevokedAt: 'Revoked at',
    endpointsTitle: 'Endpoint URLs',
    endpointsDescription: 'Base URL to use when calling the REST or MCP APIs.',
    dangerTitle: 'Danger Zone',
    dangerDescription:
      'Revoking the key invalidates it immediately and cannot be undone.',
    revokeButton: 'Revoke API Key',
    editButton: 'Edit',
  },
  createForm: {
    nameLabel: 'Name',
    namePlaceholder: 'e.g. production search service',
    nameDescription: 'A label to identify what this Key is for; display only.',
    knowledgeBasesLabel: 'Knowledge bases',
    knowledgeBasesDescription:
      'The Key can only access the selected knowledge bases. Select at least one.',
    noKnowledgeBases:
      'This Workspace has no knowledge bases yet. Create one first.',
    scopesLabel: 'Permissions',
    scopesDescription:
      'Select at least one; permissions only apply to the knowledge bases selected above.',
    expirationLabel: 'Expiration',
    expirationDescription:
      'After expiry the Key fails authentication; choose "Never" with care.',
    fixedDays: 'Fixed days',
    never: 'Never',
    daysAriaLabel: 'Validity days',
    submit: 'Create API Key',
    createdTitle: 'API Key created',
    createdDescription:
      'Copy and save the full plaintext now. You can fetch it again later from the detail page.',
    close: 'Close',
    viewDetails: 'View details',
  },
  revokeDialog: {
    title: 'Revoke API Key?',
    description:
      'The Key "{{name}}" will be revoked. Its next request will fail; already-started requests are not forcibly interrupted. This action cannot be undone.',
    cancel: 'Cancel',
    confirm: 'Revoke',
  },
  editDialog: {
    title: 'Edit API Key',
    description:
      'Change the name, knowledge bases, permissions, or expiration. Revoked keys cannot be edited.',
  },
  editForm: {
    nameLabel: 'Name',
    nameDescription: 'A label to identify what this Key is for; display only.',
    knowledgeBasesLabel: 'Knowledge bases',
    knowledgeBasesDescription:
      'The Key can only access the selected knowledge bases. Select at least one.',
    noKnowledgeBases:
      'This Workspace has no knowledge bases yet. Create one first.',
    scopesLabel: 'Permissions',
    scopesDescription:
      'Select at least one; permissions only apply to the knowledge bases selected above.',
    expirationLabel: 'Expiration',
    expirationDescription:
      'The expiry date is recomputed from the edit time; choose "Never" with care.',
    fixedDays: 'Fixed days',
    never: 'Never',
    daysAriaLabel: 'Validity days',
    submit: 'Save changes',
  },
  secretPanel: {
    reveal: 'Show plaintext',
    revealHint:
      'The plaintext is shown only temporarily in this browser; refresh or leave the page and you will need to fetch it again.',
    hide: 'Hide',
    show: 'Show',
    copied: 'Copied',
    copy: 'Copy',
    clear: 'Clear',
    copiedToast: 'API Key copied',
    copyFailedToast: 'Copy failed. Select the text manually to copy it.',
  },
  table: {
    empty: 'No API Keys yet',
    headNamePrefix: 'Name / Prefix',
    headKnowledgeBases: 'Knowledge bases',
    headScopes: 'Permissions',
    headExpiry: 'Expiry',
    headLastUsed: 'Last used',
    headStatus: 'Status',
    headView: 'View',
    noKnowledgeBases: 'None',
    neverUsed: 'Never used',
    viewAriaLabel: 'View {{name}}',
    mobileExpiry: 'Expires {{expiry}}',
    view: 'View',
  },
  display: {
    statusActive: 'Active',
    statusExpiring: 'Expiring soon',
    statusExpired: 'Expired',
    statusRevoked: 'Revoked',
    scopeKnowledgeBasesWrite: 'Knowledge base write',
    scopeDocumentsRead: 'Document read',
    scopeDocumentsWrite: 'Document write',
    scopeSearchRead: 'Search',
  },
  format: {
    never: 'Never expires',
  },
  schemas: {
    nameRequired: 'Enter a name',
    nameTooLong: 'Name must be at most 80 characters',
    daysRequired: 'Enter a valid number of days',
    daysInteger: 'Days must be an integer',
    daysMin: 'Days must be greater than 0',
    daysMax: 'Maximum 365 days',
    invalidKnowledgeBaseId: 'Invalid knowledge base ID format',
    knowledgeBaseRequired: 'Select at least one knowledge base',
    scopeRequired: 'Select at least one permission',
  },
  queries: {
    revokedToast: 'API Key revoked',
    updatedToast: 'API Key updated',
  },
} satisfies Widen<typeof zhApiKeys>
