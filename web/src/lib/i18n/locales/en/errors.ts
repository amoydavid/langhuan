import type { errors as zhErrors } from '../zh/errors'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

/**
 * 后端错误码 → 英文文案（与 zh/errors.ts 的 key 一一对应）。
 */
export const errors = {
  network_error: 'Network error. Please check your connection and retry.',
  http_error: 'Request failed. Please try again later.',
  unknown: 'The operation failed. Please try again later.',

  validation_error: 'Invalid parameters.',
  not_found: 'Resource not found.',
  unauthorized: 'Not authenticated.',
  forbidden: 'Permission denied.',
  conflict: 'Resource conflict.',
  rate_limited: 'Too many requests. Please slow down.',
  unsupported_file_type: 'Unsupported file type.',
  internal_error: 'Internal server error.',

  revision_conflict: 'Revision conflict.',
  generation_build_in_progress: 'An index generation is already being built.',
  generation_stale: 'The index generation is stale.',
  generation_not_ready: 'The index generation is not ready yet.',
  manual_edit_confirmation_required:
    'Manual edit confirmation is required for the archived revision.',
  faq_chunk_immutable: 'FAQ chunks cannot be edited directly.',
  file_tree_name_conflict: 'A sibling with the same name already exists.',
  file_tree_cycle: 'Moving the folder would create a cycle.',
  file_tree_not_empty: 'The folder is not empty.',
  unsupported_provider: 'Unsupported provider.',
  provider_scope_not_allowed: 'The provider is not allowed for this scope.',
  invalid_provider_config: 'Invalid provider configuration.',
  credentials_required: 'Credentials are required.',
  unsupported_model_type: 'Unsupported model type.',
  unsupported_embedding_dimension: 'Unsupported embedding dimension.',
  model_not_visible: 'Model not visible.',
  model_disabled: 'Model is disabled.',
  provider_disabled: 'Provider is disabled.',
  dimension_mismatch: 'The actual embedding dimension does not match.',
  connection_test_failed: 'Connection test failed.',
  authentication_failed: 'Provider authentication failed.',
  endpoint_unreachable: 'Provider endpoint is unreachable.',
  request_timeout: 'Provider request timed out.',
  provider_rejected: 'The provider rejected the request.',
  invalid_embedding_response: 'The provider returned an invalid vector.',
  immutable_model_field: 'Model semantic fields cannot be modified.',
  model_in_use: 'The model is in use.',
  provider_in_use: 'The provider is in use.',
  api_key_limit_reached: 'The active API key limit has been reached.',
  insufficient_scope: 'The API key lacks sufficient permissions.',
  api_key_secret_unavailable: 'The API key secret cannot be recovered.',
} satisfies Widen<typeof zhErrors>
