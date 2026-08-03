import { describe, expect, it } from 'vitest'
import { indexGenerationsQueryOptions } from './queries'
import { indexGenerationListSchema } from './schemas'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const generationId = '5de1f306-118b-4c2e-86f8-acde3cb6bdb4'
const workspaceId = 'f064b7d4-eba3-4d1d-8b54-b666e83d63e5'
const modelId = 'f52bec15-7278-457e-8ad6-c545bfb07c57'
const providerId = 'fdf4bde0-635e-4f61-8fb7-a3529eef88f9'

describe('indexGenerationListSchema', () => {
  it('requires an application-provided readable display label', () => {
    const [generation] = indexGenerationListSchema.parse([
      {
        id: generationId,
        workspace_id: workspaceId,
        knowledge_base_id: kbId,
        embedding_model_id: modelId,
        provider_id: providerId,
        model_name: 'text-embedding-v4',
        display_label: '2026-08-01 11:08 · text-embedding-v4 · 已就绪',
        embedding_dimension: 1024,
        chunker_version: 1,
        chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
        retrieval_config: { final_top_k: 8 },
        config_hash: 'internal-only',
        source_content_version: 18,
        indexed_content_version: 18,
        status: 'ready',
        document_count: 20,
        chunk_count: 44,
        indexed_count: 44,
        manual_edit_count: 2,
        disabled_chunk_count: 1,
        manual_edit_disposition: 'pending',
        created_at: '2026-08-01T11:08:00Z',
      },
    ])

    expect(generation?.display_label).toContain('text-embedding-v4')
    expect(generation?.display_label).not.toContain(generationId)
  })

  it('uses the exact index Generations query key', () => {
    expect(indexGenerationsQueryOptions('acme', kbId).queryKey).toEqual([
      'index-generations',
      'acme',
      kbId,
    ])
  })
})
