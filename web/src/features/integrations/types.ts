import type { z } from 'zod'
import type {
  createSourceConnectionFormSchema,
  createSourceConnectionSchema,
  sourceConnectionResponseSchema,
  sourceConnectionTestResultSchema,
  sourceProviderSchema,
  updateSourceConnectionSchema,
} from './schemas'

export type SourceProvider = z.infer<typeof sourceProviderSchema>

export type SourceConnection = z.infer<typeof sourceConnectionResponseSchema>

export type SourceConnectionTestResult = z.infer<
  typeof sourceConnectionTestResultSchema
>

export type CreateSourceConnectionInput = z.infer<
  typeof createSourceConnectionSchema
>

export type SourceConnectionFormValues = z.infer<
  typeof createSourceConnectionFormSchema
>

export type UpdateSourceConnectionInput = z.infer<
  typeof updateSourceConnectionSchema
>
