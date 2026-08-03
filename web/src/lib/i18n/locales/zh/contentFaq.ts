export const contentFaq = {
  // FAQ 表单（FAQForm）
  form: {
    conflictTitle: 'FAQ 已被他人更新',
    saveFailedTitle: '保存 FAQ 失败',
    conflictDescription:
      '你的输入仍保留在表单中。请对照最新版本后手动合并，再基于最新版本重试。',
    retryOnLatest: '基于最新版本重试',
    titleLabel: '标题',
    titlePlaceholder: '例如：退款政策',
    titleDescription: '标题用于内容列表和检索结果中的可读名称。',
    questionsTitle: '问题变体',
    questionsHint:
      '相同意图的不同问法会共享下面这一个回答。Alt + ↑/↓ 可调整顺序。',
    addQuestion: '添加问题',
    questionLabel: '问题 {{index}}',
    moveQuestionUpAriaLabel: '上移问题 {{index}}',
    moveQuestionDownAriaLabel: '下移问题 {{index}}',
    deleteQuestionAriaLabel: '删除问题 {{index}}',
    questionPlaceholder: '输入用户可能提出的问题',
    answerLabel: '回答',
    answerViewAriaLabel: '回答视图',
    viewEdit: '编辑',
    viewPreview: '预览',
    viewSplit: '并排',
    answerPlaceholder: '使用 Markdown 编写完整、可直接复用的回答…',
    previewEmptyHint: '输入回答后在这里预览。',
    answerDescription: 'Markdown 会在安全过滤后渲染；HTML 不会直接执行。',
    saveCreate: '保存 FAQ',
    saveNewVersion: '保存新版本',
  },

  // FAQ 详情（FAQDetail）
  detail: {
    statusPending: '等待处理',
    statusProcessing: '处理中',
    statusReady: '可检索',
    statusFailed: '失败',
    statusDeleting: '删除中',
    statusDeleted: '已删除',
    untitled: '未命名 FAQ',
    revisionLabel: '修订 {{no}}',
    editButton: '编辑 FAQ',
    indexingTitle: '新版本正在建立索引',
    indexingDescription:
      '当前页面会自动刷新状态；完成前，检索仍使用上一个生效版本。',
    failedTitle: 'FAQ 处理失败',
    failedFallbackMessage: '请检查内容后重新保存一个版本。',
    questionsTitle: '问题变体',
    questionsCountDescription: '{{count}} 个问法共享同一个回答',
    answerTitle: '回答',
    answerDescription: '已按安全规则渲染 Markdown',
  },

  // 版本冲突对比（FAQConflictComparison）
  conflictComparison: {
    yourVersion: '你的版本',
    latestVersion: '最新版本',
    questionsLabel: '问题变体',
    answerLabel: '回答',
  },

  // 表单校验（faqFormSchema）
  schema: {
    titleRequired: '请输入 FAQ 标题',
    questionRequired: '问题不能为空',
    questionsMin: '至少添加一个问题',
    answerRequired: '请输入回答',
    questionsDuplicate: '问题不能重复',
  },
} as const
