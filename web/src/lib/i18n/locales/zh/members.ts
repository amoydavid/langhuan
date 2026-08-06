export const members = {
  list: {
    columnUser: '用户',
    columnRole: '角色',
    columnJoinedAt: '加入时间',
    columnActions: '操作',
    unnamedUser: '未命名用户',
    empty: '暂无成员',
    role: {
      owner: '所有者',
      admin: '管理员',
      member: '成员',
    },
  },
  actions: {
    manageInvitationsButton: '管理邀请',
    adjustRoleButton: '调整角色',
    removeMemberButton: '移除成员',
    resetPasswordButton: '重置密码',
    roleDialogTitle: '调整成员角色',
    roleDialogDescription:
      '最后一名 owner 不可降级，请先将其他成员设为 owner。',
    roleLabel: '角色',
    cancelButton: '取消',
    saveRoleButton: '保存角色',
    removeDialogTitle: '移除成员',
    removeDialogDescription:
      '移除后该用户将无法继续访问此 Workspace。最后一名 owner 不可移除。',
    confirmRemoveButton: '确认移除',
    resetDialogTitle: '重置用户密码',
    resetDialogDescription: '保存后会撤销目标用户的全部旧 session。',
    newPasswordLabel: '新密码',
    confirmPasswordLabel: '确认密码',
    roleUpdatedToast: '成员角色已更新',
    memberRemovedToast: '成员已移除',
    passwordResetToast: '密码已重置，目标用户的全部旧 session 已失效',
    lastOwnerConflict:
      'Workspace 必须至少保留一名 owner，请先将其他成员设为 owner',
  },
  schemas: {
    passwordMinLength: '密码至少需要 8 个字符',
    confirmPasswordRequired: '请再次输入密码',
    passwordMismatch: '两次输入的密码不一致',
  },
} as const
