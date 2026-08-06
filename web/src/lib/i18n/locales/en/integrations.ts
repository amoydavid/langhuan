import type { integrations as zhIntegrations } from '../zh/integrations'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const integrations = {
  list: {
    eyebrow: 'Workspace / Integrations',
    title: 'Integrations',
    subtitle:
      'Manage third-party apps connected to this workspace, such as Feishu custom apps.',
    createButton: 'Add Feishu App',
    emptyTitle: 'No Feishu app added yet',
    emptyDescription:
      'Add a Feishu custom app to use its capabilities in this workspace.',
    createFirstButton: 'Add Feishu App',
    loading: 'Loading integrations…',
    providerFeishu: 'Feishu',
  },

  card: {
    appIdLabel: 'App ID',
    boundKbLabel: 'Knowledge bases',
    boundKbPlaceholder: '—',
    statusActive: 'Enabled',
    statusDisabled: 'Disabled',
    editButton: 'Edit',
    enableButton: 'Enable',
    disableButton: 'Disable',
    deleteButton: 'Delete',
  },

  form: {
    nameLabel: 'App name',
    namePlaceholder: 'e.g. Search assistant',
    appIdLabel: 'App ID',
    appIdPlaceholder: 'cli_xxxxxxxxxxxx',
    appSecretLabel: 'App Secret',
    appSecretPlaceholder: 'The app secret from the Feishu developer console',
    appSecretEditHint: 'Leave blank to keep the current App Secret.',
    submitButton: 'Test & Save',
    createdToast: 'Feishu app added',
  },

  editDialog: {
    title: 'Edit Feishu app',
    description:
      'Update the name, or rotate the App Secret (leave blank to keep it).',
    saveButton: 'Save',
    savedToast: 'Feishu app updated',
  },

  deleteDialog: {
    title: 'Delete Feishu app',
    description:
      'Once deleted, this app can no longer be called. This action cannot be undone.',
    confirm: 'Confirm delete',
    cancel: 'Cancel',
    deletedToast: 'Feishu app deleted',
  },

  toggle: {
    enabledToast: 'Feishu app enabled',
    disabledToast: 'Feishu app disabled',
  },

  status: {
    active: 'Enabled',
    disabled: 'Disabled',
  },
} satisfies Widen<typeof zhIntegrations>
