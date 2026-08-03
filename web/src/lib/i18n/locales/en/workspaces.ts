import type { workspaces as zhWorkspaces } from '../zh/workspaces'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const workspaces = {
  form: {
    nameLabel: 'Name',
    slugDescription:
      'Used in the URL. Supports lowercase letters, numbers and hyphens, and should stay stable after creation.',
    createButton: 'Create workspace',
    createdToast: 'Workspace created',
    adminOnlyTitle: 'Only platform admins can create workspaces',
    adminOnlyDescription:
      'Contact your platform administrator to request a new workspace.',
    nameRequired: 'Please enter a name',
    slugMinLength: 'Slug must be at least 3 characters',
    slugMaxLength: 'Slug must be at most 64 characters',
    slugFormat:
      'Only lowercase letters, numbers and hyphens are allowed, and the slug cannot start or end with a hyphen',
  },
  overview: {
    loadingLabel: 'Loading Workspace overview',
    eyebrow: 'Workspace overview',
    description:
      'Continue from the current readiness state without needing to understand the internal index structure first.',
  },
  picker: {
    eyebrow: 'Workspaces',
    title: 'Choose a workspace',
    description:
      'Each workspace independently manages its knowledge bases, documents, members and invitations.',
    createButton: 'Create workspace',
    emptyTitle: 'No accessible workspace yet',
    emptyAdminDescription:
      'Create your first workspace to start managing knowledge content.',
    emptyMemberDescription:
      'Ask a platform administrator to create a workspace or invite you to one.',
    enterWorkspace: 'Enter {{name}}',
    roles: {
      owner: 'Owner',
      admin: 'Admin',
      member: 'Member',
    },
  },
} satisfies Widen<typeof zhWorkspaces>
