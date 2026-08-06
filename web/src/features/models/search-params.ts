import { z } from 'zod'

export const modelServiceSearchSchema = z.object({
  view: z.enum(['models', 'connections']).catch('models').default('models'),
  type: z.enum(['all', 'embedding', 'rerank']).catch('all').default('all'),
  capability: z
    .enum(['all', 'embedding', 'rerank', 'parser'])
    .catch('all')
    .default('all'),
  status: z.enum(['all', 'active', 'disabled']).catch('all').default('all'),
  scope: z.enum(['all', 'workspace', 'platform']).catch('all').default('all'),
  q: z.string().max(100).catch('').default(''),
})

export type ModelServiceSearch = z.infer<typeof modelServiceSearchSchema>
