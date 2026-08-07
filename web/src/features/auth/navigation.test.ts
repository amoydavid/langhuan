import { describe, expect, it } from 'vitest'
import {
  chooseWorkspaceEntry,
  safeRedirect,
  shouldCoordinateUnauthorized,
} from './navigation'
import type { MeResponse } from './types'

const baseMe: MeResponse = {
  user: {
    id: '10000000-0000-0000-0000-000000000001',
    email: 'admin@example.com',
    nickname: '管理员',
    is_platform_admin: true,
  },
  workspaces: [],
  single_tenant: false,
}

describe('safeRedirect', () => {
  it('accepts only same-site absolute paths', () => {
    expect(safeRedirect('/workspaces/acme/kb')).toBe('/workspaces/acme/kb')
    expect(safeRedirect('/sign-in')).toBeUndefined()
    expect(safeRedirect('/setup')).toBeUndefined()
    expect(safeRedirect('//evil.example')).toBeUndefined()
    expect(safeRedirect('https://evil.example')).toBeUndefined()
    expect(safeRedirect('workspaces/acme')).toBeUndefined()
  })
})

describe('shouldCoordinateUnauthorized', () => {
  it('ignores expected session probes on public authentication routes', () => {
    expect(shouldCoordinateUnauthorized('/')).toBe(false)
    expect(shouldCoordinateUnauthorized('/setup')).toBe(false)
    expect(shouldCoordinateUnauthorized('/sign-in')).toBe(false)
    expect(shouldCoordinateUnauthorized('/invitations/token')).toBe(false)
  })

  it('coordinates expired sessions on authenticated routes', () => {
    expect(shouldCoordinateUnauthorized('/workspaces/acme/kb')).toBe(true)
    expect(shouldCoordinateUnauthorized('/settings/appearance')).toBe(true)
  })
})

describe('chooseWorkspaceEntry', () => {
  const workspaces: MeResponse['workspaces'] = [
    {
      workspace_id: '20000000-0000-0000-0000-000000000002',
      slug: 'acme',
      name: 'Acme',
      role: 'owner',
    },
    {
      workspace_id: '30000000-0000-0000-0000-000000000003',
      slug: 'beta',
      name: 'Beta',
      role: 'member',
    },
  ]

  it('enters the only workspace directly', () => {
    expect(
      chooseWorkspaceEntry(
        { ...baseMe, workspaces: [workspaces[0]] },
        undefined
      )
    ).toBe('/workspaces/acme/kb')
  })

  it('prefers a valid recent workspace', () => {
    expect(chooseWorkspaceEntry({ ...baseMe, workspaces }, 'beta')).toBe(
      '/workspaces/beta/kb'
    )
  })

  it('uses the picker for multiple or zero workspaces', () => {
    expect(chooseWorkspaceEntry({ ...baseMe, workspaces }, 'missing')).toBe(
      '/workspaces'
    )
    expect(chooseWorkspaceEntry(baseMe, undefined)).toBe('/workspaces')
  })
})
