import type { Model } from '@/features/models/types'

export type SearchSettingsRerank = {
  model_id: string
  provider_id: string
  model_name: string
  candidate_top_k: number
  failure_mode: 'fallback' | 'fail'
}

export type WorkspaceSearchSettings = {
  workspace_id: string
  rerank: SearchSettingsRerank | null
  updated_at: string
}

export type UpdateWorkspaceSearchSettingsInput = {
  rerank: {
    enabled: boolean
    model_id?: string
    candidate_top_k?: number
    failure_mode?: 'fallback' | 'fail'
  }
}

export type SearchSettingsModel = Pick<
  Model,
  'id' | 'display_name' | 'model_name' | 'provider'
>
