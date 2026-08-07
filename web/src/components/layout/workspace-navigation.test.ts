import { describe, expect, it } from 'vitest'
import { buildWorkspaceNavigation } from './workspace-navigation'

describe('buildWorkspaceNavigation', () => {
  it('splits workspace and admin entries into separate groups for admins', () => {
    const groups = buildWorkspaceNavigation('acme', 'admin', true)

    expect(groups.map((group) => group.title)).toEqual([
      '工作区',
      '工作区管理',
      '平台管理',
    ])
  })

  it('keeps high-frequency business entries in the workspace group for all roles', () => {
    for (const role of ['member', 'admin', 'owner'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)
      const workspace = groups[0]
      expect(workspace?.title).toBe('工作区')
      expect(workspace?.items.map((item) => item.title)).toEqual([
        '概览',
        '知识库',
        '模型',
      ])
    }
  })

  it('places admin entries in a dedicated management group in the agreed order', () => {
    for (const role of ['owner', 'admin'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)
      const adminGroup = groups[1]
      expect(adminGroup?.title).toBe('工作区管理')
      expect(adminGroup?.items.map((item) => item.title)).toEqual([
        '成员',
        '集成',
        'API 密钥',
        '检索策略',
      ])
    }
  })

  it('does not expose the management group to workspace members', () => {
    const groups = buildWorkspaceNavigation('acme', 'member', false)

    expect(groups).toHaveLength(1)
    expect(groups[0]?.title).toBe('工作区')
    expect(groups[0]?.items.map((item) => item.title)).toEqual([
      '概览',
      '知识库',
      '模型',
    ])
  })

  it('uses the localized API key title instead of the hardcoded string', () => {
    for (const role of ['owner', 'admin'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)
      const titles = groups[1]?.items.map((item) => item.title) ?? []
      expect(titles).toContain('API 密钥')
      expect(titles).not.toContain('API Key')
    }
  })

  it('exposes platform administration in its own group only for platform admins', () => {
    const groups = buildWorkspaceNavigation('acme', 'admin', true)
    const platform = groups[2]
    expect(platform?.title).toBe('平台管理')
    expect(platform?.items).toEqual([
      expect.objectContaining({ title: '平台模型', url: '/admin/models' }),
    ])
  })

  it('does not expose platform administration to non-platform admins', () => {
    const groups = buildWorkspaceNavigation('acme', 'owner', false)
    expect(groups.map((group) => group.title)).not.toContain('平台管理')
  })

  it('keeps invitations out of the workspace sidebar', () => {
    for (const role of ['owner', 'admin'] as const) {
      const groups = buildWorkspaceNavigation('acme', role, false)
      const allTitles = groups.flatMap((group) =>
        group.items.map((item) => item.title)
      )
      expect(allTitles).not.toContain('邀请')
    }
  })
})
