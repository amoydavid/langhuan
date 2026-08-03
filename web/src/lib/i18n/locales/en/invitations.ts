import type { invitations as zhInvitations } from '../zh/invitations'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const invitations = {
  list: {
    columnEmail: 'Invited email',
    columnRole: 'Role',
    columnStatus: 'Status',
    columnTokenPrefix: 'Token prefix',
    columnExpiresAt: 'Expires at',
    columnActions: 'Actions',
    revokeButton: 'Revoke',
    tokenPrefixValue: 'Token prefix: {{prefix}}',
    empty: 'No invitations yet',
    status: {
      pending: 'Pending',
      accepted: 'Accepted',
      expired: 'Expired',
      revoked: 'Revoked',
    },
  },
  form: {
    emailLabel: 'Invited email',
    roleLabel: 'Role',
    role: {
      member: 'Member',
      admin: 'Administrator',
      owner: 'Owner',
    },
    submitButton: 'Send invitation',
    createdDialogTitle: 'Invitation created',
    createdDialogDescription:
      'This full link is only returned in the create response.',
    linkNotVisibleAgain:
      'You will not be able to view the full link again after closing',
    closeButton: 'Close',
    copiedButton: 'Copied',
    copyLinkButton: 'Copy invitation link',
    linkCopiedToast: 'Invitation link copied',
  },
  revoke: {
    dialogTitle: 'Revoke invitation',
    dialogDescription:
      'After revocation, this invitation link can no longer be used.',
    cancelButton: 'Cancel',
    confirmButton: 'Confirm revocation',
    successToast: 'Invitation revoked',
  },
  schemas: {
    invalidEmail: 'Please enter a valid email address',
  },
} satisfies Widen<typeof zhInvitations>
