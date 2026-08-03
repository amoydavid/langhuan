import { describe, expect, it } from 'vitest'
import { apiKeyCreateSchema, apiKeyUpdateSchema } from './schemas'

const validKBId = '10000000-0000-4000-8000-000000000001'
const otherKBId = '20000000-0000-4000-8000-000000000002'

const validBase = {
  name: '生产检索服务',
  knowledge_base_ids: [validKBId],
  scopes: ['documents:read', 'search:read'],
  expiration: { type: 'days', days: 90 },
}

describe('apiKeyCreateSchema', () => {
  it('parses a valid days-expiring payload', () => {
    const parsed = apiKeyCreateSchema.parse(validBase)
    expect(parsed.name).toBe('生产检索服务')
    expect(parsed.expiration.type).toBe('days')
    expect(parsed.expiration).toMatchObject({ type: 'days', days: 90 })
  })

  it('parses a never-expiring payload', () => {
    const parsed = apiKeyCreateSchema.parse({
      ...validBase,
      expiration: { type: 'never' },
    })
    expect(parsed.expiration.type).toBe('never')
  })

  it('trims the name and rejects empty names', () => {
    const { name } = apiKeyCreateSchema.parse({
      ...validBase,
      name: '  带空格的名称  ',
    })
    expect(name).toBe('带空格的名称')

    const result = apiKeyCreateSchema.safeParse({ ...validBase, name: '   ' })
    expect(result.success).toBe(false)
  })

  it('rejects names longer than 80 characters', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      name: '名'.repeat(81),
    })
    expect(result.success).toBe(false)
  })

  it('requires at least one knowledge base', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      knowledge_base_ids: [],
    })
    expect(result.success).toBe(false)
  })

  it('deduplicates and keeps multiple knowledge bases', () => {
    const { knowledge_base_ids } = apiKeyCreateSchema.parse({
      ...validBase,
      knowledge_base_ids: [validKBId, otherKBId],
    })
    expect(knowledge_base_ids).toEqual([validKBId, otherKBId])
  })

  it('requires at least one scope', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      scopes: [],
    })
    expect(result.success).toBe(false)
  })

  it('rejects an unknown scope value', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      scopes: ['admin:all'],
    })
    expect(result.success).toBe(false)
  })

  it('accepts all four canonical scopes', () => {
    const { scopes } = apiKeyCreateSchema.parse({
      ...validBase,
      scopes: [
        'knowledge_bases:write',
        'documents:read',
        'documents:write',
        'search:read',
      ],
    })
    expect(scopes).toHaveLength(4)
  })

  it('rejects a malformed knowledge base id', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      knowledge_base_ids: ['not-a-uuid'],
    })
    expect(result.success).toBe(false)
  })

  it('rejects days expiration with non-positive days', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      expiration: { type: 'days', days: 0 },
    })
    expect(result.success).toBe(false)
  })

  it('rejects days expiration exceeding 365', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      expiration: { type: 'days', days: 366 },
    })
    expect(result.success).toBe(false)
  })

  it('rejects an unknown expiration type', () => {
    const result = apiKeyCreateSchema.safeParse({
      ...validBase,
      expiration: { type: 'months', months: 3 },
    })
    expect(result.success).toBe(false)
  })
})

// apiKeyUpdateSchema 复用与 create 相同的字段校验，这里聚焦关键路径，
// 避免与 create 用例重复。
describe('apiKeyUpdateSchema', () => {
  it('parses a valid payload', () => {
    const parsed = apiKeyUpdateSchema.parse(validBase)
    expect(parsed.name).toBe('生产检索服务')
    expect(parsed.expiration).toMatchObject({ type: 'days', days: 90 })
  })

  it('parses a never-expiring payload', () => {
    const parsed = apiKeyUpdateSchema.parse({
      ...validBase,
      expiration: { type: 'never' },
    })
    expect(parsed.expiration.type).toBe('never')
  })

  it('requires at least one knowledge base', () => {
    const result = apiKeyUpdateSchema.safeParse({
      ...validBase,
      knowledge_base_ids: [],
    })
    expect(result.success).toBe(false)
  })

  it('requires at least one scope', () => {
    const result = apiKeyUpdateSchema.safeParse({
      ...validBase,
      scopes: [],
    })
    expect(result.success).toBe(false)
  })

  it('rejects an unknown scope value', () => {
    const result = apiKeyUpdateSchema.safeParse({
      ...validBase,
      scopes: ['admin:all'],
    })
    expect(result.success).toBe(false)
  })

  it('rejects an empty name', () => {
    const result = apiKeyUpdateSchema.safeParse({ ...validBase, name: '   ' })
    expect(result.success).toBe(false)
  })

  it('rejects days expiration exceeding 365', () => {
    const result = apiKeyUpdateSchema.safeParse({
      ...validBase,
      expiration: { type: 'days', days: 366 },
    })
    expect(result.success).toBe(false)
  })
})
