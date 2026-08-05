import { describe, expect, it } from 'vitest'
import { knowledgeBaseResponseSchema } from './schemas'

describe('knowledgeBaseResponseSchema', () => {
  it('parses active-Generation-derived model, chunking and retrieval config', () => {
    const parsed = knowledgeBaseResponseSchema.parse({
      id: '10000000-0000-4000-8000-000000000001',
      workspace_id: '20000000-0000-4000-8000-000000000002',
      name: '产品知识库',
      description: '',
      embedding_model_id: '30000000-0000-4000-8000-000000000003',
      embedding_model: {
        id: '30000000-0000-4000-8000-000000000003',
        name: 'embed',
        display_name: 'Embedding',
        provider: 'openai',
        provider_display_name: 'OpenAI',
        dimensions: 1024,
        available: true,
      },
      chunking_config: {
        strategy: 'auto',
        enable_parent_child: true,
        parent_chunk_size: 4096,
        child_chunk_size: 384,
        chunk_size: 512,
        chunk_overlap: 80,
      },
      retrieval_config: {
        fts_config: 'simple',
        vector_top_k: 30,
        keyword_top_k: 30,
        final_top_k: 10,
        rrf_k: 60,
      },
      content_version: 3,
      active_index_generation_id: '40000000-0000-4000-8000-000000000004',
      file_tree_root_id: '50000000-0000-4000-8000-000000000005',
      metadata: {},
      created_at: '2026-07-31T00:00:00Z',
      updated_at: '2026-07-31T00:00:00Z',
    })

    expect(parsed.embedding_model.dimensions).toBe(1024)
    expect(parsed.chunking_config.chunk_size).toBe(512)
    expect(parsed.retrieval_config.final_top_k).toBe(10)
  })
})
