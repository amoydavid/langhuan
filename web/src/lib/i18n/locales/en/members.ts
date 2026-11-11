import type { members as zhMembers } from '../zh/members'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const members = {
  list: {
    columnUser: 'User',
    columnRole: 'Role',
    columnKbScope: 'Knowledge bases',
    columnJoinedAt: 'Joined',
    columnActions: 'Actions',
    unnamedUser: 'Unnamed user',
    empty: 'No members yet',
    kbScopeAll: 'All',
    kbScopeCounted: '{{count}} knowledge bases',
    role: {
      owner: 'Owner',
      admin: 'Administrator',
      member: 'Member',
    },
  },
  actions: {
    manageInvitationsButton: 'Manage invitations',
    adjustRoleButton: 'Adjust role',
    kbGrantsButton: 'Knowledge base access',
    removeMemberButton: 'Remove member',
    resetPasswordButton: 'Reset password',
    kbGrantsDialogTitle: 'Knowledge base access',
    kbGrantsScopeLabel: 'Access scope',
    kbGrantsScopeAll: 'All knowledge bases',
    kbGrantsScopeAllHint:
      'Can access every knowledge base in the workspace, now and in the future',
    kbGrantsScopeRestricted: 'Selected knowledge bases only',
    kbGrantsScopeRestrictedHint:
      'Can only access the knowledge bases checked below',
    kbGrantsSelectedCount: 'Selected {{selected}} / {{total}}',
    kbGrantsEmptyWarning:
      'Checking none = this member will see no knowledge bases',
    kbGrantsMemberOnlyHint:
      'Knowledge base access applies to members only (admins/owners always see all)',
    kbGrantsLoading: 'Loading knowledge bases…',
    kbGrantsSaveButton: 'Save',
    kbGrantsUpdatedToast: 'Knowledge base access updated',
    roleDialogTitle: 'Adjust member role',
    roleDialogDescription:
      'The last owner cannot be demoted. Promote another member to owner first.',
    roleLabel: 'Role',
    cancelButton: 'Cancel',
    saveRoleButton: 'Save role',
    removeDialogTitle: 'Remove member',
    removeDialogDescription:
      'This user will no longer be able to access this Workspace after removal. The last owner cannot be removed.',
    confirmRemoveButton: 'Confirm removal',
    resetDialogTitle: 'Reset user password',
    resetDialogDescription:
      "All of the user's old sessions will be revoked after saving.",
    newPasswordLabel: 'New password',
    confirmPasswordLabel: 'Confirm password',
    roleUpdatedToast: 'Member role updated',
    memberRemovedToast: 'Member removed',
    passwordResetToast:
      "Password reset; all of the user's old sessions have been revoked",
    lastOwnerConflict:
      'A Workspace must keep at least one owner. Promote another member to owner first.',
  },
  schemas: {
    passwordMinLength: 'Password must be at least 8 characters',
    confirmPasswordRequired: 'Please enter the password again',
    passwordMismatch: 'The two passwords do not match',
    kbGrantsIdsRequired: 'Pick at least one knowledge base (or switch to all)',
  },
} satisfies Widen<typeof zhMembers>
