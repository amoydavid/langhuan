import { describe, expect, it } from 'vitest'
import { workspaceReadinessQueryOptions } from './queries'
import { workspaceReadinessSchema } from './schemas'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const documentId = '087a124b-859f-4786-902a-2dd1901a006f'

describe('workspaceReadinessSchema', () => {
  it('parses the server-selected next action and readable targets', () => {
    const result = workspaceReadinessSchema.parse({
      has_active_provider: true,
      has_selectable_embedding_model: true,
      knowledge_base_count: 2,
      document_counts: { total: 20, ready: 18, processing: 1, failed: 1 },
      searchable_knowledge_base_count: 1,
      recommended_action: 'resolve_failed_document',
      recommended_knowledge_base_id: kbId,
      recommended_knowledge_base_name: '产品文档',
      recommended_document_id: documentId,
      recommended_document_name: 'faq-import.csv',
    })

    expect(result.recommended_knowledge_base_name).toBe('产品文档')
    expect(result.recommended_document_name).toBe('faq-import.csv')
  })

  it('rejects unknown recommendation actions', () => {
    expect(() =>
      workspaceReadinessSchema.parse({
        has_active_provider: false,
        has_selectable_embedding_model: false,
        knowledge_base_count: 0,
        document_counts: { total: 0, ready: 0, processing: 0, failed: 0 },
        searchable_knowledge_base_count: 0,
        recommended_action: 'train_model',
        recommended_knowledge_base_id: null,
        recommended_knowledge_base_name: '',
        recommended_document_id: null,
        recommended_document_name: '',
      })
    ).toThrow()
  })

  it('uses the stable Workspace readiness query key', () => {
    expect(workspaceReadinessQueryOptions('acme').queryKey).toEqual([
      'workspace-readiness',
      'acme',
    ])
  })
})
