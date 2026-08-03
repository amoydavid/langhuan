import { describe, expect, it } from 'vitest'
import {
  contentQueryKey,
  fileTreeQueryKey,
  knowledgeBaseSummaryQueryOptions,
} from './queries'
import { knowledgeBaseSummarySchema } from './schemas'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const generationId = '5de1f306-118b-4c2e-86f8-acde3cb6bdb4'

describe('knowledgeBaseSummarySchema', () => {
  it('parses readable workbench state without requiring internal metadata', () => {
    const result = knowledgeBaseSummarySchema.parse({
      knowledge_base_id: kbId,
      knowledge_base_name: '产品文档',
      content_version: 18,
      document_counts: {
        total: 20,
        file: 14,
        faq: 5,
        web: 1,
        ready: 18,
        processing: 1,
        failed: 1,
      },
      active_generation: {
        id: generationId,
        display_label: '2026-08-01 09:42 · text-embedding-3-large · 当前生效',
        status: 'ready',
        model_display_name: 'text-embedding-3-large',
        embedding_dimension: 3584,
        chunker_version: 1,
        chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
        retrieval_config: { final_top_k: 8 },
        source_content_version: 18,
        indexed_content_version: 18,
        document_count: 20,
        chunk_count: 38,
        indexed_count: 38,
        manual_edit_count: 2,
        disabled_chunk_count: 1,
        created_at: '2026-08-01T09:42:00Z',
      },
      candidate_generation: null,
      sync_state: 'failed',
      recent_jobs: [],
      blockers: [],
    })

    expect(result.knowledge_base_name).toBe('产品文档')
    expect(result.active_generation?.display_label).toContain('当前生效')
  })

  it('uses the exact summary, content and File Tree query keys', () => {
    const filters = { kind: 'file' as const, status: 'ready' as const }

    expect(knowledgeBaseSummaryQueryOptions('acme', kbId).queryKey).toEqual([
      'knowledge-base-summary',
      'acme',
      kbId,
    ])
    expect(contentQueryKey('acme', kbId, filters)).toEqual([
      'content',
      'acme',
      kbId,
      filters,
    ])
    expect(fileTreeQueryKey('acme', kbId)).toEqual(['file-tree', 'acme', kbId])
  })
})
