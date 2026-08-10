/**
 * 路由页面（web/src/routes）与错误页（web/src/features/errors）的文案。
 */
export const routes = {
  // 认证相关页面（(auth)/）
  auth: {
    setup: {
      title: '初始化琅嬛',
      description: '创建首位平台管理员。完成后请使用该账号登录。',
    },
    invitation: {
      loading: '正在读取邀请…',
      invalid: '邀请不存在、已失效或已被使用。',
      title: '加入琅嬛',
      description: '你受邀加入「{{name}}」，请设置昵称和登录密码。',
    },
  },

  // 错误页（(errors)/ 与 features/errors）
  errors: {
    back: '返回上一页',
    home: '返回首页',
    forbidden: {
      title: '无权访问',
      description: '当前账号没有查看此资源所需的权限。',
    },
    unauthorized: {
      title: '需要登录',
      description: '请先使用有效账号登录，再访问此资源。',
    },
    notFound: {
      title: '页面不存在',
      description: '你访问的页面不存在，或已经被移除。',
    },
    general: {
      title: '系统暂时无法完成请求',
      description: '请稍后重试；如果问题持续存在，请联系管理员。',
    },
    maintenance: {
      title: '服务正在维护',
      description: '管理台当前不可用，请稍后再试。',
      reload: '重新加载',
    },
  },

  // 平台管理（_authenticated/admin）
  admin: {
    breadcrumb: '平台管理',
    models: {
      breadcrumb: '平台模型',
      detail: { breadcrumb: '连接详情' },
    },
  },

  // Workspace 相关页面（_authenticated/workspaces）
  workspaces: {
    breadcrumb: '工作空间',
    workspace: {
      breadcrumb: '工作区',
    },
    new: {
      breadcrumb: '创建工作区',
      eyebrow: '平台管理',
      title: '创建工作区',
      description: '工作区是知识库、文档和成员权限的隔离边界。',
      cardTitle: '基本信息',
      cardDescription: 'Slug 将出现在管理台地址中，请使用稳定、易读的值。',
    },
    overview: { breadcrumb: '概览' },
    members: {
      breadcrumb: '成员',
      eyebrow: 'Workspace 权限',
      title: '成员',
      description: '角色决定成员在此 Workspace 中可以执行的操作。',
    },
    invitations: {
      breadcrumb: '邀请',
      eyebrow: 'Workspace 权限',
      title: '邀请',
      description:
        '完整邀请链接只在创建成功时返回一次，历史列表仅保留 Token 前缀。',
    },
    notFound: {
      title: 'Workspace 不存在或无权访问',
      description: '请检查地址，或返回 Workspace 列表选择你可以访问的空间。',
    },
    documents: {
      detail: { breadcrumb: '文档详情' },
    },
    jobs: {
      detail: { breadcrumb: '处理任务' },
    },
    models: {
      breadcrumb: '模型',
      detail: { breadcrumb: '连接详情' },
    },
    searchSettings: {
      breadcrumb: '检索策略',
      eyebrow: 'Workspace 配置',
      title: '检索策略',
      description:
        '配置此 Workspace 单库和多库 knowledge_search 使用的全局 Rerank 策略。',
    },
    apiKeys: {
      breadcrumb: 'API Key',
      new: { breadcrumb: '创建 API Key' },
      detail: {
        breadcrumb: '密钥详情',
        notFoundTitle: 'API Key 不存在或无权访问',
        notFoundDescription:
          '请检查地址，或返回当前 Workspace 的 API Key 列表。',
      },
    },

    // 集成应用（integrations）
    integrations: {
      breadcrumb: '集成',
      new: {
        breadcrumb: '添加飞书应用',
        eyebrow: '工作区 / 集成',
        title: '添加飞书应用',
        description: '填入飞书开放平台的应用凭证，保存后即可在工作区中调用。',
        cardTitle: '应用凭证',
        cardDescription:
          'App Secret 仅用于后端调用飞书 API，不会在页面上回显。',
      },
    },

    // 知识库（kb）
    kb: {
      breadcrumb: '知识库',
      new: {
        breadcrumb: '创建知识库',
        eyebrow: '知识处理',
        title: '创建知识库',
        description: '选择一个当前可用的 Embedding 模型，并设置文档分块规则。',
        cardTitle: '知识库配置',
        cardDescription: '默认分块配置为 512 / 80，可根据文档结构调整。',
      },
      detail: {
        breadcrumb: '知识库',
        loading: '正在加载知识库工作台',
        notFoundTitle: '知识库不存在或无权访问',
        notFoundDescription: '请检查地址，或返回当前 Workspace 的知识库列表。',
      },
      documents: {
        new: { breadcrumb: '上传文档' },
      },
      indexes: {
        breadcrumb: '索引',
        buildStartedToast: '索引版本已开始构建',
        activatedToast: '索引版本已激活',
        reindexStartedToast: '重建索引已开始，构建完成后需手动激活',
        title: '索引版本',
        description: '新版本构建期间，检索继续使用当前生效版本。',
        buildButton: '构建新索引版本',
        reindexButton: '重建索引',
        candidateTitle: '构建候选索引版本',
      },
      settings: {
        breadcrumb: '设置',
        savedToast: '知识库基本信息已更新',
      },
      search: {
        breadcrumb: '检索测试',
        noIndexTitle: '当前没有可用索引',
        noIndexDescription: '请先添加内容并等待索引构建完成，再运行检索测试。',
      },
      content: {
        breadcrumb: '内容',
        all: {
          breadcrumb: '全部内容',
          searchPlaceholder: '搜索标题…',
          searchAriaLabel: '搜索内容',
          statusAriaLabel: '内容状态',
          statusAll: '全部状态',
          statusReady: '可检索',
          statusProcessing: '处理中',
          statusFailed: '失败',
          statusDeleting: '删除中',
          statusDeleted: '已删除',
          sortAriaLabel: '内容排序',
          sortUpdated: '最近更新',
          sortName: '名称',
          uploadButton: '上传文件',
          newFaqButton: '新建 FAQ',
        },
        faq: {
          breadcrumb: 'FAQ',
          searchAriaLabel: '搜索 FAQ',
          searchPlaceholder: '搜索 FAQ 标题…',
          statusAriaLabel: 'FAQ 状态',
          statusAll: '全部状态',
          statusReady: '可检索',
          statusProcessing: '处理中',
          statusFailed: '失败',
          newButton: '新建 FAQ',
          new: {
            breadcrumb: '新建 FAQ',
            title: '新建 FAQ',
            description: '用一组用户问法维护一个经过安全渲染的 Markdown 回答。',
            backToList: '返回 FAQ 列表',
          },
          detail: {
            breadcrumb: 'FAQ 详情',
            unnamedTitle: '未命名 FAQ',
          },
          edit: {
            breadcrumb: '编辑 FAQ',
            title: '编辑「{{title}}」',
            unnamedTitle: '未命名 FAQ',
            description:
              '保存会创建不可变的新修订；检索索引完成前继续使用上一个版本。',
            backToDetail: '返回详情',
            savedToast: '新版本正在建立索引',
          },
        },
        files: {
          breadcrumb: '文件',
          selectTitle: '选择一个文件',
          selectDescription:
            '从左侧文件树打开文件，查看规范化预览和当前索引版本的有效分块。',
          upload: {
            breadcrumb: '上传文件',
            title: '上传文件',
            description:
              '上传完成后会进入独立文件页面；解析与索引在后台异步执行。',
            cardTitle: '选择文件',
            cardDescription: '支持的文件会保存到当前选中目录。',
          },
          detail: {
            breadcrumb: '文件详情',
            unnamedTitle: '未命名文件',
            confirmLeave: '有未保存的分块修订，确定离开吗？',
            showOnlySearchable: '仅显示参与检索的分块',
            viewJob: '查看处理任务',
            viewChunks: '查看分块（{{count}}）',
            sheetTitle: '{{name}} 的分块',
            sheetDescription:
              '查看当前索引版本的有效分块、原始来源和修订历史。',
            dialogTitle: '创建分块修订',
            dialogDescription: '保存会创建新版本，并由后台更新当前索引。',
            chunkPanelLabel: '分块',
            chunkDetailTitle: '分块详情',
            chunkDetailDescription: '查看当前内容、原始来源与修订历史。',
          },
        },
        web: {
          breadcrumb: 'Web',
          searchAriaLabel: '搜索 Web 内容',
          searchPlaceholder: '搜索 Web 标题…',
          statusAriaLabel: 'Web 状态',
          statusAll: '全部状态',
          statusReady: '可检索',
          statusProcessing: '处理中',
          statusFailed: '失败',
          detail: {
            breadcrumb: 'Web 详情',
            unnamedTitle: '未命名 Web 文档',
          },
        },
      },
    },
  },
} as const
