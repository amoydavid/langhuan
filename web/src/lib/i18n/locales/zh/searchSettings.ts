export const searchSettings = {
  form: {
    enabledLabel: '启用全局 Rerank',
    enabledDescription:
      '在各知识库召回并合并后，使用一个模型统一重排候选结果。',
    modelLabel: 'Rerank 模型',
    modelPlaceholder: '选择 Rerank 模型',
    modelDescription: '仅显示当前 Workspace 可见且已启用的 Rerank 模型。',
    candidateLabel: '候选数量',
    candidateDescription: '进入 Rerank 的候选数量，范围 50–200。',
    failureLabel: '失败策略',
    failureFallback: '回退到 RRF',
    failureFail: '直接返回错误',
    failureDescription: '远端服务暂时不可用时的处理方式。',
    scopeDescription:
      '此策略应用于当前 Workspace 的单库和多库 knowledge_search。只有 Workspace 管理员可以修改。',
    save: '保存策略',
    saved: '检索策略已保存',
  },
} as const
