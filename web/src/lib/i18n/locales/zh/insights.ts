/**
 * 健康洞察（v1.4.0 spec §9）：工作台「健康」tab——计数卡、零召回查询、
 * 沉睡内容与内容动态。en 资源见 locales/en/insights.ts（结构一致）。
 */
export const insights = {
  breadcrumb: '健康',

  page: {
    scopeNote: '口径：不含管理台检索测试',
    windowAriaLabel: '时间范围',
    window7: '近 7 天',
    window14: '近 14 天',
    window30: '近 30 天',
  },

  empty: {
    title: '还没有检索数据',
    description:
      '通过 MCP、REST 或检索测试完成查询后，这里会显示知识库的健康状况。',
  },

  member: {
    title: '健康洞察仅工作区管理员可见',
    description:
      '空召回率、沉睡内容与内容动态面向知识库维护者。如需查看，请联系工作区管理员或所有者。',
  },

  cards: {
    runsTitle: '近 {{window}} 天检索',
    runsSub: '空召回 {{empty}} · 降级 {{degraded}}',
    emptyRateTitle: '空召回率',
    emptyRatePrevious: '上期 {{rate}}',
    emptyRateNoPrevious: '暂无上期对比',
    deltaDown: '较上期 -{{delta}}pp',
    deltaUp: '较上期 +{{delta}}pp',
    sleepingTitle: '沉睡内容',
    sleepingSub: '近 30 天零引用',
    sleepingInsufficient: '检索样本不足，暂无法判定',
    changesTitle: '近 {{window}} 天变更',
    changesSub: '内容新版本与移除事件',
  },

  zeroRecall: {
    title: '零召回查询',
    description: '近 {{window}} 天未返回任何内容的检索问法（按问法聚合）',
    columnQuery: '查询内容',
    columnMasked: '查询摘要（脱敏）',
    columnHits: '次数',
    columnFirstSeen: '首次出现',
    columnLastSeen: '最近出现',
    columnActions: '操作',
    maskedQuery: '•••（{{chars}} 字）',
    createFaq: '创建 FAQ',
    testSearch: '检索测试',
    disabledTitle: '未开启查询原文记录',
    disabledDescription:
      '目前只能看到次数与趋势，无法定位具体问法；开启后可对零召回问题直接补写 FAQ。',
    enableButton: '开启记录',
    emptyTitle: '近 {{window}} 天没有零召回查询',
    emptyHint: '知识库对近期问题都能返回内容，这是好状态。',
    enableDialog: {
      title: '开启查询原文记录',
      description:
        '开启后，该知识库的检索问题原文将保留 30 天，仅工作区管理员可读，可随时关闭；关闭前的历史原文会随保留期自动清除。',
      cancel: '取消',
      confirm: '开启记录',
      successToast: '查询原文记录已开启',
    },
  },

  sleeping: {
    title: '沉睡内容',
    description: '近 30 天未出现在任何检索结果 · 已索引满 30 天',
    columnName: '名称',
    columnKind: '类型',
    columnStatus: '状态',
    columnIndexdAt: '更新时间',
    columnActions: '操作',
    statusReady: '可检索',
    viewAriaLabel: '查看 {{name}}',
    insufficientTitle: '检索样本不足',
    insufficientDescription:
      '近 30 天完成检索少于 10 次，暂无法判定沉睡内容；检索量提升后会自动恢复。',
    emptyTitle: '没有沉睡内容',
    emptyHint: '近 30 天的检索覆盖了全部已索引文档。',
    kind: {
      file: '文件',
      web: 'Web',
      faq: 'FAQ',
    },
  },

  activity: {
    title: '内容动态',
    description: '全部来源 · 最近 {{count}} 条',
    badgeSync: '同步',
    badgeRevision: '变更',
    badgeDelete: '移除',
    syncSuccess: '{{name}} 同步成功',
    syncFailed: '{{name}} 同步失败',
    revisionReason: {
      ingest: '上传了新版本',
      replace: '上传了新版本',
      crawl: '抓取更新',
      edit: '编辑了内容',
      reparse: '重新解析',
      fallback: '更新了内容',
    },
    revisionLine: '{{title}} {{reason}}',
    deleteLine: '{{title}} 已移出检索',
    empty: '最近没有内容变更',
  },
} as const
