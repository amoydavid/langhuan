import { describe, expect, it } from 'vitest'
import { retrievalTestQueryKey } from './queries'
import { retrievalResultsSchema } from './schemas'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const documentId = '087a124b-859f-4786-902a-2dd1901a006f'
const revisionId = '87e1d974-5e2f-48f6-9445-3ef076bc5b32'
const chunkId = 'ae34e7f5-8732-4c2a-985f-6a030231eb3c'

describe('retrievalResultsSchema', () => {
  it('parses readable source evidence and branch scores', () => {
    const [result] = retrievalResultsSchema.parse([
      {
        chunk_id: chunkId,
        chunk_revision_id: revisionId,
        document_id: documentId,
        document_kind: 'file',
        content: 'Docker 部署时，通过 DATABASE_DSN 指定 PostgreSQL。',
        document_name: 'installation.md',
        source_anchor: {
          source_type: 'markdown',
          line_start: 24,
          line_end: 31,
        },
        score: 0.0325,
        vector_score: 0.84,
        keyword_score: 12.31,
        ranking_stage: 'rrf',
        metadata: {},
        matched_children: [
          {
            chunk_id: chunkId,
            chunk_revision_id: revisionId,
            role: 'child',
            source_anchor: {
              source_type: 'markdown',
              line_start: 24,
              line_end: 31,
            },
            score: 0.0325,
            vector_score: 0.84,
            keyword_score: 12.31,
          },
        ],
      },
    ])

    expect(result?.document_name).toBe('installation.md')
    expect(result?.source_anchor.line_start).toBe(24)
  })

  it('uses the complete request in the exact retrieval query key', () => {
    const request = {
      query: '如何配置数据库？',
      vector_top_k: 20,
      keyword_top_k: 20,
      final_top_k: 8,
    }
    expect(retrievalTestQueryKey('acme', kbId, request)).toEqual([
      'retrieval-test',
      'acme',
      kbId,
      request,
    ])
  })
})
