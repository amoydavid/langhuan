import type { z } from 'zod'
import type {
  createSourceConnectionSchema,
  sourceConnectionResponseSchema,
  updateSourceConnectionSchema,
} from './schemas'

export type SourceConnection = z.infer<typeof sourceConnectionResponseSchema>

export type CreateSourceConnectionInput = z.infer<
  typeof createSourceConnectionSchema
>

export type UpdateSourceConnectionInput = z.infer<
  typeof updateSourceConnectionSchema
>
