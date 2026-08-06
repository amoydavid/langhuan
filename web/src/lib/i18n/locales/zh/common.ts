export const common = {
  // 品牌名
  brandName: '琅嬛',

  // 跨页面通用动作 / 状态文案
  cancel: '取消',
  loading: '正在加载…',
  sessionExpired: '登录已过期，请重新登录',
  forbidden: '权限不足',
  invalidApiBaseUrl: 'VITE_API_BASE_URL 必须以 /api/v1 结尾',
  appearanceSettings: '外观设置',
  signOut: '退出登录',
  signOutDescription: '退出后需要重新登录才能访问控制台。',
  openUserMenu: '打开用户菜单',
  breadcrumbsAriaLabel: '面包屑',
  languageSwitchAriaLabel: '切换语言',

  // 命令面板（CommandMenu）
  commandMenu: {
    searchPlaceholder: '搜索导航、Workspace 与知识库…',
    noResults: '没有匹配结果',
    knowledgeBases: '知识库',
    quickActions: '快捷动作',
    theme: '主题',
    createKnowledgeBase: '创建知识库',
    uploadFileToKb: '上传文件到「{{name}}」',
    createFaq: '创建 FAQ',
    openSearchTest: '打开检索测试',
    light: '浅色',
    dark: '深色',
    system: '跟随系统',
  },

  // 布局（侧栏 / 顶栏）
  layout: {
    searchPlaceholder: '搜索导航与知识库',
    navOverview: '概览',
    navKnowledgeBases: '知识库',
    navModels: '模型',
    navMembers: '成员',
    navSearchSettings: '检索策略',
    navIntegrations: '集成',
    navInvitations: '邀请',
    navWorkspace: '工作区',
    navPlatformAdmin: '平台管理',
    navPlatformModels: '平台模型',
    noWorkspace: '暂无 Workspace',
    createWorkspacePrompt: '请先创建工作区',
    roleLabel: '角色：{{role}}',
    createWorkspace: '创建工作区',
  },
} as const
