export type ModelScope = 'workspace' | 'platform'
export type ModelStatus = 'active' | 'disabled'
export type ModelType = 'embedding'
export type ProviderKey =
  | 'openai'
  | 'ark'
  | 'ollama'
  | 'dashscope'
  | 'tencentcloud'

export const embeddingDimensions = [798, 1024, 2048, 3584] as const
export type EmbeddingDimension = (typeof embeddingDimensions)[number]

export const modelFormDefaults = {
  name: '',
  display_name: '',
  description: '',
  model_name: '',
  dimensions: 1024 as EmbeddingDimension,
  batch_size: 32,
  truncate: false,
  keep_alive_seconds: undefined as number | undefined,
}

export type ModelProvider = {
  id: string
  scope: ModelScope
  workspace_id?: string
  name: string
  display_name: string
  description: string
  provider: ProviderKey
  config: Record<string, unknown>
  credentials_configured: boolean
  credential_fields: string[]
  status: ModelStatus
  created_at: string
  updated_at: string
}

export type ModelProviderSummary = {
  id: string
  scope: ModelScope
  workspace_id?: string
  name: string
  display_name: string
  provider: ProviderKey
  status: ModelStatus
}

export type Model = {
  id: string
  provider_id: string
  provider: ModelProviderSummary
  name: string
  display_name: string
  description: string
  type: ModelType
  model_name: string
  dimensions: EmbeddingDimension
  parameters: Record<string, unknown>
  status: ModelStatus
  reference_count: number
  available: boolean
  created_at: string
  updated_at: string
}

type ModelProviderInputBase = {
  name: string
  display_name: string
  description: string
}

export type CreateModelProviderInput = ModelProviderInputBase &
  (
    | {
        provider: 'openai'
        config: {
          mode: 'standard' | 'azure'
          base_url?: string
          api_version?: string
          timeout_seconds: number
        }
        credentials: {
          api_key: string
          custom_headers?: Record<string, string>
        }
      }
    | {
        provider: 'ark'
        config: {
          base_url?: string
          region: string
          auth_mode: 'api_key' | 'ak_sk'
          timeout_seconds: number
          retry_times: number
        }
        credentials:
          | { api_key: string }
          | { access_key: string; secret_key: string }
      }
    | {
        provider: 'ollama'
        config: { base_url: string; timeout_seconds: number }
        credentials: Record<string, never>
      }
    | {
        provider: 'dashscope'
        config: { timeout_seconds: number }
        credentials: { api_key: string }
      }
    | {
        provider: 'tencentcloud'
        config: { region: string }
        credentials: { secret_id: string; secret_key: string }
      }
  )

export type UpdateModelProviderInput = {
  display_name?: string
  description?: string
  config?: CreateModelProviderInput['config']
  credentials?: CreateModelProviderInput['credentials']
  status?: ModelStatus
}

export type CreateModelInput = {
  name: string
  display_name: string
  description: string
  type: ModelType
  model_name: string
  dimensions: EmbeddingDimension
  parameters: Record<string, unknown>
}

export type UpdateModelInput = {
  display_name?: string
  description?: string
  model_name?: string
  dimensions?: EmbeddingDimension
  parameters?: Record<string, unknown>
  status?: ModelStatus
}

export type ConnectionTestResult = {
  ok: boolean
  dimensions: number
  duration_ms: number
}
