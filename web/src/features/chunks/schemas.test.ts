import { describe, expect, it } from 'vitest'
import {
  chunkQueryOptions,
  chunkRevisionsQueryOptions,
  documentChunksQueryOptions,
} from './queries'
import { documentChunkPageSchema } from './schemas'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const documentId = '087a124b-859f-4786-902a-2dd1901a006f'
const generationId = '5de1f306-118b-4c2e-86f8-acde3cb6bdb4'
const revisionId = '87e1d974-5e2f-48f6-9445-3ef076bc5b32'
const chunkId = 'ae34e7f5-8732-4c2a-985f-6a030231eb3c'
const chunkSetId = 'c373dee3-3cfc-42b6-985c-f9b46807b97d'
const workspaceId = 'f064b7d4-eba3-4d1d-8b54-b666e83d63e5'

describe('documentChunkPageSchema', () => {
  it('parses a disabled active revision and its readable editor name', () => {
    const result = documentChunkPageSchema.parse({
      generation_id: generationId,
      document_revision_id: revisionId,
      chunk_set_id: chunkSetId,
      items: [
        {
          id: chunkId,
          workspace_id: workspaceId,
          knowledge_base_id: kbId,
          document_id: documentId,
          document_revision_id: revisionId,
          chunk_set_id: chunkSetId,
          sequence: 0,
          source_content: '原始来源',
          source_anchor: {
            source_type: 'markdown',
            line_start: 24,
            line_end: 31,
          },
          metadata: {},
          active_revision: {
            id: revisionId,
            chunk_id: chunkId,
            revision_no: 3,
            content: '人工修订内容',
            context_header: '安装指南 > Docker 部署',
            enabled: false,
            status: 'ready',
            edit_source: 'user',
            editor_display_name: '林墨',
            created_at: '2026-08-01T10:00:00Z',
          },
          created_at: '2026-08-01T09:00:00Z',
        },
      ],
      next_cursor: null,
    })

    expect(result.items[0]?.active_revision?.enabled).toBe(false)
    expect(result.items[0]?.active_revision?.editor_display_name).toBe('林墨')
  })

  it('uses generation-aware document and stable Chunk query keys', () => {
    const filters = { enabled: false, limit: 50 }
    expect(
      documentChunksQueryOptions(
        'acme',
        kbId,
        documentId,
        generationId,
        filters
      ).queryKey
    ).toEqual([
      'document-chunks',
      'acme',
      kbId,
      documentId,
      generationId,
      filters,
    ])
    expect(chunkQueryOptions('acme', kbId, chunkId).queryKey).toEqual([
      'chunk',
      'acme',
      kbId,
      chunkId,
    ])
    expect(chunkRevisionsQueryOptions('acme', kbId, chunkId).queryKey).toEqual([
      'chunk-revisions',
      'acme',
      kbId,
      chunkId,
    ])
  })
})
