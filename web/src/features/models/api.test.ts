import { describe, expect, it } from 'vitest'
import {
  modelCollectionPath,
  modelProviderCollectionPath,
  modelResourcePath,
} from './api'

describe('model API paths', () => {
  it('derives scope from the route instead of request data', () => {
    expect(modelProviderCollectionPath('workspace', 'acme / cn')).toBe(
      '/workspaces/acme%20%2F%20cn/model-providers'
    )
    expect(modelProviderCollectionPath('platform')).toBe(
      '/admin/model-providers'
    )
    expect(modelCollectionPath('workspace', 'provider/id', 'acme')).toBe(
      '/workspaces/acme/model-providers/provider%2Fid/models'
    )
    expect(modelResourcePath('platform', 'model/id')).toBe(
      '/admin/models/model%2Fid'
    )
  })
})
