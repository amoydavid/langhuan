import { describe, expect, it } from 'vitest'
import { buildWorkspaceNavigation } from './workspace-navigation'

describe('buildWorkspaceNavigation', () => {
  it('keeps workspace and platform model entries in separate groups', () => {
    const groups = buildWorkspaceNavigation('acme', 'admin', true)

    expect(groups.map((group) => group.title)).toEqual(['工作区', '平台管理'])
    expect(groups[0]?.items).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          title: '模型',
          url: '/workspaces/acme/models',
        }),
      ])
    )
    expect(groups[1]?.items).toEqual([
      expect.objectContaining({ title: '平台模型', url: '/admin/models' }),
    ])
  })

  it('does not expose platform administration to workspace members', () => {
    const groups = buildWorkspaceNavigation('acme', 'member', false)

    expect(groups).toHaveLength(1)
    expect(groups[0]?.items).toEqual(
      expect.arrayContaining([expect.objectContaining({ title: '模型' })])
    )
  })

  it('exposes the API Key entry to owner/admin only', () => {
    for (const role of ['owner', 'admin'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)
      expect(groups[0]?.items).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            title: 'API Key',
            url: '/workspaces/acme/api-keys',
          }),
          expect.objectContaining({
            title: '检索策略',
            url: '/workspaces/acme/search-settings',
          }),
        ])
      )
    }
  })

  it('exposes the Integrations entry to owner/admin only', () => {
    for (const role of ['owner', 'admin'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)
      expect(groups[0]?.items).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            title: '集成',
            url: '/workspaces/acme/integrations',
          }),
        ])
      )
    }
  })

  it('keeps invitations out of the workspace sidebar', () => {
    for (const role of ['owner', 'admin'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)

      expect(groups[0]?.items).not.toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            title: '邀请',
            url: '/workspaces/acme/invitations',
          }),
        ])
      )
    }
  })

  it('hides configuration entries from workspace members', () => {
    const groups = buildWorkspaceNavigation('acme', 'member', false)
    expect(groups[0]?.items).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ title: 'API Key' })])
    )
    expect(groups[0]?.items).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ title: '检索策略' })])
    )
    expect(groups[0]?.items).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ title: '集成' })])
    )
  })
})
