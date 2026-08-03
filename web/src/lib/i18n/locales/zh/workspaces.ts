export const workspaces = {
  form: {
    nameLabel: '名称',
    slugDescription: '用于 URL，支持小写字母、数字和连字符，创建后应保持稳定。',
    createButton: '创建工作区',
    createdToast: '工作区已创建',
    adminOnlyTitle: '仅平台管理员可以创建工作区',
    adminOnlyDescription: '如需新的工作区，请联系平台管理员。',
    nameRequired: '请输入名称',
    slugMinLength: 'Slug 至少需要 3 个字符',
    slugMaxLength: 'Slug 最多 64 个字符',
    slugFormat: '仅支持小写字母、数字和连字符，且不能以连字符开头或结尾',
  },
  overview: {
    loadingLabel: '正在加载 Workspace 概览',
    eyebrow: 'Workspace 概览',
    description: '从当前准备状态继续工作，不需要先理解内部索引结构。',
  },
  picker: {
    eyebrow: '工作空间',
    title: '选择 Workspace',
    description: '每个 Workspace 独立管理知识库、文档、成员和邀请。',
    createButton: '创建工作区',
    emptyTitle: '暂无可访问的 Workspace',
    emptyAdminDescription: '创建第一个工作区，开始管理知识内容。',
    emptyMemberDescription: '请联系平台管理员创建工作区或邀请你加入。',
    enterWorkspace: '进入 {{name}}',
    roles: {
      owner: '所有者',
      admin: '管理员',
      member: '成员',
    },
  },
} as const
