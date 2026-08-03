import type { members as zhMembers } from '../zh/members'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const members = {
  list: {
    columnUser: 'User',
    columnRole: 'Role',
    columnJoinedAt: 'Joined',
    columnActions: 'Actions',
    unnamedUser: 'Unnamed user',
    empty: 'No members yet',
    role: {
      owner: 'Owner',
      admin: 'Administrator',
      member: 'Member',
    },
  },
  actions: {
    adjustRoleButton: 'Adjust role',
    removeMemberButton: 'Remove member',
    resetPasswordButton: 'Reset password',
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
  },
} satisfies Widen<typeof zhMembers>
