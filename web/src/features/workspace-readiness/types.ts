import type { z } from 'zod'
import type { readinessActionSchema, workspaceReadinessSchema } from './schemas'

export type ReadinessAction = z.infer<typeof readinessActionSchema>
export type WorkspaceReadiness = z.infer<typeof workspaceReadinessSchema>
