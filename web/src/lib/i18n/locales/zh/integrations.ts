export const integrations = {
  // 集成列表页
  list: {
    eyebrow: '工作区 / 集成',
    title: '集成应用',
    subtitle: '管理接入到本工作区的第三方应用，如飞书自建应用。',
    createButton: '添加飞书应用',
    emptyTitle: '尚未添加飞书应用',
    emptyDescription: '添加飞书自建应用后，即可在本工作区中使用其能力。',
    createFirstButton: '添加飞书应用',
    loading: '正在加载集成应用…',
    providerFeishu: '飞书',
  },

  // 源连接卡片
  card: {
    appIdLabel: 'App ID',
    boundKbLabel: '绑定知识库',
    boundKbPlaceholder: '—',
    statusActive: '已启用',
    statusDisabled: '已停用',
    editButton: '编辑',
    enableButton: '启用',
    disableButton: '停用',
    deleteButton: '删除',
  },

  // 添加飞书应用表单
  form: {
    nameLabel: '应用名称',
    namePlaceholder: '例如：检索助手',
    appIdLabel: 'App ID',
    appIdPlaceholder: 'cli_xxxxxxxxxxxx',
    appSecretLabel: 'App Secret',
    appSecretPlaceholder: '飞书开放平台获取的应用密钥',
    appSecretEditHint: '留空表示不修改当前 App Secret。',
    submitButton: '测试并保存',
    createdToast: '飞书应用已添加',
  },

  // 编辑对话框
  editDialog: {
    title: '编辑飞书应用',
    description: '修改名称，或轮换 App Secret（留空保持不变）。',
    saveButton: '保存',
    savedToast: '飞书应用已更新',
  },

  // 删除确认对话框
  deleteDialog: {
    title: '删除飞书应用',
    description: '删除后该应用将无法再被调用，此操作不可恢复。',
    confirm: '确认删除',
    cancel: '取消',
    deletedToast: '飞书应用已删除',
  },

  // 启用 / 停用提示
  toggle: {
    enabledToast: '飞书应用已启用',
    disabledToast: '飞书应用已停用',
  },

  // 状态相关
  status: {
    active: '已启用',
    disabled: '已停用',
  },
} as const
