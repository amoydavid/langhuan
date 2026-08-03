export const knowledgeBases = {
  // 知识库列表页
  list: {
    eyebrow: '知识处理',
    title: '知识库',
    subtitle: '每个知识库显式绑定 Embedding 模型，并独立维护分块规则。',
    createButton: '创建知识库',
    emptyTitle: '还没有知识库',
    emptyDescription: '创建知识库后即可上传并处理文档。',
    createFirstButton: '创建第一个知识库',
    noDescription: '暂无描述',
    viewAriaLabel: '查看 {{name}}',
    modelUnavailable: '已不可用',
    embeddingMeta:
      '{{provider}} · {{dimensions}} 维 · 分块 {{chunkSize}} / 重叠 {{chunkOverlap}}',
  },

  // 创建知识库表单
  form: {
    nameLabel: '名称',
    descriptionLabel: '描述',
    chunkSizeLabel: '分块大小',
    chunkOverlapLabel: '重叠大小',
    createdToast: '知识库已创建',
    submitButton: '创建知识库',
  },

  // Embedding 模型选择器
  embeddingModelSelect: {
    label: 'Embedding 模型',
    placeholder: '请选择模型',
    workspaceGroup: '当前 Workspace',
    platformGroup: '平台共享',
    noModelsManageable: '当前没有可用的 Embedding 模型。',
    configureModelsLink: '先配置模型',
    noModelsMember: '请联系 Workspace 管理员配置模型。',
    option: '{{name}} · {{provider}} · {{dimensions}} 维',
  },

  // 知识库概览页
  overview: {
    countTotal: '全部内容',
    countReady: '可检索',
    countProcessing: '处理中',
    countFailed: '失败',
    activeIndexTitle: '当前索引版本',
    activeModelLine: '{{modelName}} · {{dimensions}} 维',
    syncLabel: '索引同步',
    contentVersion: '内容版本 {{version}}',
    indexedVersion: '已索引 {{version}}',
    scaleLabel: '索引规模',
    documentScale: '{{documentCount}} 篇文档 · {{chunkCount}} 个分块',
    missingActiveTitle: '缺少当前索引版本',
    missingActiveDescription: '这不是普通空状态，请检查知识库配置。',
    blockersTitle: '需要处理',
    noBlockersTitle: '没有阻断事项',
    noBlockersDescription: '当前内容和索引状态正常。',
    recentJobsTitle: '最近任务',
    recentJobsDescription: '真实后台活动，不展示原始 payload。',
    noRecentJobs: '暂无最近任务',
    addContentButton: '添加内容',
    searchTestButton: '检索测试',
    buildIndexButton: '构建新索引版本',
    checkCandidateButton: '检查候选版本',
    jobStatus: {
      pending: '等待中',
      queued: '已排队',
      running: '运行中',
      completed: '已完成',
      succeeded: '已完成',
      failed: '失败',
      cancelled: '已取消',
    },
  },

  // 知识库设置页
  settings: {
    nameRequired: '请输入知识库名称',
    basicsTitle: '基本信息',
    basicsDescription: '名称和描述用于页面展示，不会改变索引内容。',
    readOnlyTitle: '只读设置',
    readOnlyDescription: '知识库基本信息由 Workspace 管理员维护',
    nameLabel: '知识库名称',
    descriptionLabel: '知识库描述',
    saveBasicsButton: '保存基本信息',
    configTitle: '当前检索配置',
    configDescription: '修改模型、分块或检索参数需要构建新的索引版本。',
    modelLabel: 'Embedding 模型',
    dimensionLabel: '维度',
    chunkingLabel: '分块',
    retrievalLabel: '候选 / 最终',
    noActiveVersion: '当前没有生效的索引版本。',
    buildIndexButton: '构建新索引版本',
    diagnosticsTitle: '诊断信息',
    diagnosticsDescription: '内部标识不会显示在页面上；仅在需要排障时复制。',
    copyDiagnostics: '复制诊断信息',
    copiedDiagnostics: '已复制诊断信息',
  },

  // 知识库工作台布局
  workbench: {
    unnamedName: '未命名知识库',
    noDescription: '暂无描述',
    contentMeta: '内容版本 {{version}} · {{chunkCount}} 个分块',
    addContentButton: '添加内容',
    areaAriaLabel: '知识库区域',
    tabs: {
      overview: '概览',
      content: '内容',
      search: '检索测试',
      indexes: '索引',
      settings: '设置',
    },
    syncState: {
      synced: '已同步',
      updating: '更新中',
      failed: '有失败',
      candidate_ready: '候选待激活',
    },
  },

  // zod 校验消息
  schemas: {
    nameRequired: '请输入名称',
    embeddingModelRequired: '请选择 Embedding 模型',
    chunkSizeInvalid: '请输入分块大小',
    chunkSizeInteger: '分块大小必须是整数',
    chunkSizePositive: '分块大小必须大于 0',
    chunkOverlapInvalid: '请输入重叠大小',
    chunkOverlapInteger: '重叠大小必须是整数',
    chunkOverlapMin: '重叠大小不能小于 0',
    chunkOverlapLessThanSize: '重叠大小必须小于分块大小',
  },
} as const
