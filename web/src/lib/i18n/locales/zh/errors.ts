/**
 * 后端错误码 → 中文文案。
 * 文案与 internal/domain/errors + internal/interfaces/http/errors.go 保持一致；
 * 前端展示错误时按 code 取文案，不再直接透传后端 message。
 */
export const errors = {
  // 前端自身错误
  network_error: '网络连接失败，请检查后重试',
  http_error: '请求失败，请稍后重试',
  unknown: '操作失败，请稍后重试',

  // HTTP 状态码映射（codeForStatus）
  validation_error: '参数校验失败',
  not_found: '资源不存在',
  unauthorized: '未认证',
  forbidden: '无权限',
  conflict: '资源冲突',
  rate_limited: '请求过于频繁',
  unsupported_file_type: '不支持的文件类型',
  internal_error: '服务器内部错误',

  // modelErrorDetails / 领域错误
  revision_conflict: '修订版本冲突',
  generation_build_in_progress: '已有索引代次正在构建',
  generation_stale: '索引代次已过期',
  generation_not_ready: '索引代次尚未就绪',
  manual_edit_confirmation_required: '需要确认归档人工编辑',
  faq_chunk_immutable: 'FAQ 分块不可直接修改',
  file_tree_name_conflict: '同级名称冲突',
  file_tree_cycle: '文件树移动会形成环',
  file_tree_not_empty: '目录非空',
  unsupported_provider: '不支持的 Provider',
  provider_scope_not_allowed: 'Provider 不允许用于此作用域',
  invalid_provider_config: 'Provider 配置无效',
  credentials_required: '需要配置凭证',
  unsupported_model_type: '不支持的模型类型',
  unsupported_embedding_dimension: '不支持的 Embedding 维度',
  model_not_visible: '模型不可见',
  model_disabled: '模型已停用',
  provider_disabled: 'Provider 已停用',
  dimension_mismatch: 'Embedding 实际维度不匹配',
  connection_test_failed: '连接测试失败',
  authentication_failed: '供应商认证失败',
  endpoint_unreachable: '供应商地址不可达',
  request_timeout: '供应商请求超时',
  provider_rejected: '供应商拒绝请求',
  invalid_embedding_response: '供应商返回了无效向量',
  immutable_model_field: '模型语义字段不可修改',
  model_in_use: '模型正在使用',
  provider_in_use: 'Provider 正在使用',
  api_key_limit_reached: '活跃 API Key 数量已达上限',
  insufficient_scope: 'API Key 权限不足',
  api_key_secret_unavailable: 'API Key 明文不可恢复',
} as const
