export const integrations = {
  // 集成列表页
  list: {
    eyebrow: '工作区 / 集成',
    title: '集成应用',
    subtitle:
      '管理接入到本工作区的外部内容源连接（飞书 / Firecrawl / Jina / 渲染浏览器）。',
    createButton: '新建连接',
    emptyTitle: '尚未添加任何连接',
    emptyDescription:
      '添加飞书、Firecrawl、Jina 或渲染浏览器（CDP）连接，供网站内容源抓取网页使用。',
    createFirstButton: '新建连接',
    loading: '正在加载集成应用…',
    providerFeishu: '飞书',
    providerFirecrawl: 'Firecrawl',
    providerJina: 'Jina',
    providerBrowser: '渲染浏览器',
  },

  // 源连接卡片
  card: {
    appIdLabel: 'App ID',
    endpointLabel: 'Endpoint',
    endpointDefault: '官方默认端点',
    apiKeyLabel: 'API Key',
    keyMasked: '••••••••',
    boundKbLabel: '绑定知识库',
    boundKbPlaceholder: '—',
    statusActive: '已启用',
    statusDisabled: '已停用',
    testButton: '测试连接',
    editButton: '编辑',
    enableButton: '启用',
    disableButton: '停用',
    deleteButton: '删除',
  },

  // 新建连接表单
  form: {
    providerLabel: '连接类型',
    providerFeishu: '飞书',
    providerFirecrawl: 'Firecrawl',
    providerJina: 'Jina',
    providerBrowser: '渲染浏览器（CDP）',
    nameLabel: '应用名称',
    namePlaceholder: '例如：检索助手',
    appIdLabel: 'App ID',
    appIdPlaceholder: 'cli_xxxxxxxxxxxx',
    appSecretLabel: 'App Secret',
    appSecretPlaceholder: '飞书开放平台获取的应用密钥',
    apiKeyLabel: 'API Key',
    apiKeyPlaceholder: '服务商控制台获取的 API Key',
    endpointLabel: 'Endpoint URL',
    endpointFirecrawlPlaceholder:
      'https://api.firecrawl.dev（官方默认，可留空）',
    endpointBrowserPlaceholder: 'ws://127.0.0.1:9222',
    tokenLabel: 'Token',
    tokenPlaceholder: 'CDP 调试端口令牌（可选）',
    appSecretEditHint: '留空表示不修改当前凭证。',
    submitButton: '保存',
    createdToast: '连接已添加',
    createdToastTestHint: '可在列表卡片中点击「测试连接」验证连通性。',
  },

  // 编辑对话框
  editDialog: {
    title: '编辑连接',
    description: '修改名称，或轮换访问凭证（留空保持不变）。',
    saveButton: '保存',
    savedToast: '连接已更新',
  },

  // 删除确认对话框
  deleteDialog: {
    title: '删除连接',
    description: '删除后该连接将无法再被调用，此操作不可恢复。',
    confirm: '确认删除',
    cancel: '取消',
    deletedToast: '连接已删除',
  },

  // 启用 / 停用提示
  toggle: {
    enabledToast: '连接已启用',
    disabledToast: '连接已停用',
  },

  // 状态相关
  status: {
    active: '已启用',
    disabled: '已停用',
  },
} as const
