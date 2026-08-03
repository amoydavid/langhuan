export const workspaceReadiness = {
  recentKnowledgeBase: '最近的知识库',
  relatedContent: '相关内容',
  nextAction: {
    waitingAdmin: '等待管理员配置',
    provider: {
      eyebrow: '下一步 · 模型连接',
      title: '配置可用的模型连接',
      description: '知识处理需要至少一个当前 Workspace 可见的活动 Provider。',
      label: '配置模型连接',
      memberTitle: '模型连接尚未就绪',
      memberDescription: '请联系 Workspace 管理员配置模型连接。',
    },
    embeddingModel: {
      eyebrow: '下一步 · Embedding 模型',
      title: '创建可用于知识库的 Embedding 模型',
      description: '选择受支持的向量维度，连接就绪后即可创建知识库。',
      label: '创建 Embedding 模型',
      memberTitle: 'Embedding 模型尚未就绪',
      memberDescription: '请联系 Workspace 管理员创建可用的 Embedding 模型。',
    },
    knowledgeBase: {
      eyebrow: '下一步 · 知识库',
      title: '创建第一个知识库',
      description: '为一组相关内容选择模型与分块配置。',
      label: '创建知识库',
    },
    addContent: {
      eyebrow: '下一步 · {{kbName}}',
      title: '为「{{kbName}}」添加内容',
      description: '上传文件或创建 FAQ，内容会在后台完成处理与索引。',
      label: '添加内容',
    },
    waiting: {
      eyebrow: '处理中 · {{kbName}}',
      title: '等待「{{documentName}}」完成处理',
      description: '页面只显示后台返回的真实状态；完成后即可验证检索。',
      label: '查看处理状态',
    },
    failed: {
      eyebrow: '需要处理 · {{kbName}}',
      description: '内容处理失败，请查看安全错误摘要后重新导入或删除内容。',
      label: '处理失败内容',
    },
    testRetrieval: {
      eyebrow: '下一步 · {{kbName}}',
      title: '为「{{kbName}}」运行一次检索测试',
      description: '确认当前索引能返回正确、可追溯的来源证据。',
      label: '开始检索',
    },
    complete: {
      eyebrow: '准备完成',
      title: 'Workspace 已具备知识检索条件',
      description: '可以继续添加内容、验证检索或维护现有知识库。',
    },
  },
  panel: {
    title: '准备状态',
    description: '由当前 Workspace 的真实资源状态计算。',
  },
  rows: {
    provider: {
      label: '模型连接',
      configured: '已配置',
      notConfigured: '尚未配置',
    },
    embeddingModel: {
      label: 'Embedding 模型',
      available: '存在可选择模型',
      unavailable: '尚无可选择模型',
    },
    knowledgeBase: {
      label: '知识库',
      count: '{{count}} 个',
    },
    content: {
      label: '内容',
      detail:
        '{{ready}} 条可检索 · {{processing}} 条处理中 · {{failed}} 条失败',
    },
    retrieval: {
      label: '检索验证',
      ready: '{{count}} 个知识库已具备条件',
      waiting: '等待可检索内容',
    },
  },
  recent: {
    title: '最近知识库',
    emptyTitle: '还没有知识库',
    emptyDescription: '创建后即可添加文件或 FAQ。',
    unnamed: '未命名知识库',
    noDescription: '暂无描述',
  },
  quickLinks: {
    title: '常用入口',
    newKnowledgeBase: '新建知识库',
    models: '模型',
    membersAndInvitations: '成员与邀请',
    members: '成员',
    searchTest: '检索测试',
  },
} as const
