import type { z } from 'zod'
import type {
  chunkEditSourceSchema,
  chunkRevisionListSchema,
  chunkRevisionSchema,
  chunkRevisionStatusSchema,
  chunkSchema,
  documentChunkPageSchema,
} from './schemas'

export type ChunkRevisionStatus = z.infer<typeof chunkRevisionStatusSchema>
export type ChunkEditSource = z.infer<typeof chunkEditSourceSchema>
export type ChunkRevision = z.infer<typeof chunkRevisionSchema>
export type Chunk = z.infer<typeof chunkSchema>
export type DocumentChunkPage = z.infer<typeof documentChunkPageSchema>
export type ChunkRevisionList = z.infer<typeof chunkRevisionListSchema>

export type DocumentChunkFilters = {
  enabled?: boolean
  cursor?: string
  limit?: number
}

export type CreateChunkRevisionInput = {
  base_revision_id: string
  content: string
  context_header: string
  enabled: boolean
}
