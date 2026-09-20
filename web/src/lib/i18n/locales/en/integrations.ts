import type { integrations as zhIntegrations } from '../zh/integrations'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const integrations = {
  list: {
    eyebrow: 'Workspace / Integrations',
    title: 'Integrations',
    subtitle:
      'Manage external content source connections for this workspace (Feishu / Firecrawl / Jina / rendering browser).',
    createButton: 'New Connection',
    emptyTitle: 'No connections yet',
    emptyDescription:
      'Add a Feishu, Firecrawl, Jina, or rendering browser (CDP) connection for web content sources to fetch pages.',
    createFirstButton: 'New Connection',
    loading: 'Loading integrations…',
    providerFeishu: 'Feishu',
    providerFirecrawl: 'Firecrawl',
    providerJina: 'Jina',
    providerBrowser: 'Rendering browser',
  },

  card: {
    appIdLabel: 'App ID',
    endpointLabel: 'Endpoint',
    endpointDefault: 'Official default endpoint',
    apiKeyLabel: 'API Key',
    keyMasked: '••••••••',
    boundKbLabel: 'Knowledge bases',
    boundKbPlaceholder: '—',
    statusActive: 'Enabled',
    statusDisabled: 'Disabled',
    testButton: 'Test connection',
    editButton: 'Edit',
    enableButton: 'Enable',
    disableButton: 'Disable',
    deleteButton: 'Delete',
  },

  form: {
    providerLabel: 'Connection type',
    providerFeishu: 'Feishu',
    providerFirecrawl: 'Firecrawl',
    providerJina: 'Jina',
    providerBrowser: 'Rendering browser (CDP)',
    nameLabel: 'App name',
    namePlaceholder: 'e.g. Search assistant',
    appIdLabel: 'App ID',
    appIdPlaceholder: 'cli_xxxxxxxxxxxx',
    appSecretLabel: 'App Secret',
    appSecretPlaceholder: 'The app secret from the Feishu developer console',
    apiKeyLabel: 'API Key',
    apiKeyPlaceholder: 'The API key from your provider console',
    endpointLabel: 'Endpoint URL',
    endpointFirecrawlPlaceholder:
      'https://api.firecrawl.dev (official default, optional)',
    endpointBrowserPlaceholder: 'ws://127.0.0.1:9222',
    tokenLabel: 'Token',
    tokenPlaceholder: 'CDP debug port token (optional)',
    appSecretEditHint: 'Leave blank to keep the current credential.',
    submitButton: 'Save',
    createdToast: 'Connection added',
    createdToastTestHint:
      'Use "Test connection" on the connection card to verify connectivity.',
  },

  editDialog: {
    title: 'Edit connection',
    description:
      'Update the name, or rotate the access credential (leave blank to keep it).',
    saveButton: 'Save',
    savedToast: 'Connection updated',
  },

  deleteDialog: {
    title: 'Delete connection',
    description:
      'Once deleted, this connection can no longer be called. This action cannot be undone.',
    confirm: 'Confirm delete',
    cancel: 'Cancel',
    deletedToast: 'Connection deleted',
  },

  toggle: {
    enabledToast: 'Connection enabled',
    disabledToast: 'Connection disabled',
  },

  status: {
    active: 'Enabled',
    disabled: 'Disabled',
  },
} satisfies Widen<typeof zhIntegrations>
