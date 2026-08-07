import type { MeResponse } from './types'

export const LAST_WORKSPACE_SLUG_KEY = 'langhuan:last-workspace-slug'

const publicAuthPaths = new Set(['/', '/setup', '/sign-in'])

export function shouldCoordinateUnauthorized(pathname: string) {
  return !publicAuthPaths.has(pathname) && !pathname.startsWith('/invitations/')
}

export function safeRedirect(raw: string | undefined) {
  if (!raw?.startsWith('/') || raw.startsWith('//')) return undefined
  if (raw.includes('\\')) return undefined
  for (const character of raw) {
    const code = character.charCodeAt(0)
    if (code <= 31 || code === 127) return undefined
  }
  const pathname = raw.split(/[?#]/, 1)[0]
  if (!shouldCoordinateUnauthorized(pathname)) return undefined
  return raw
}

/**
 * 整页跳转到站内路径（next 来自服务端校验过的路径，必须是站内绝对路径）。
 * 独立成模块函数以便测试替换，避免直接依赖不可 mock 的 window.location。
 */
export function hardRedirect(path: string) {
  window.location.assign(path)
}

export function workspaceEntry(slug: string) {
  return `/workspaces/${encodeURIComponent(slug)}/kb`
}

export function chooseWorkspaceEntry(
  me: MeResponse,
  recentSlug: string | undefined
) {
  if (
    recentSlug &&
    me.workspaces.some((workspace) => workspace.slug === recentSlug)
  ) {
    return workspaceEntry(recentSlug)
  }
  if (me.workspaces.length === 1) {
    return workspaceEntry(me.workspaces[0].slug)
  }
  return '/workspaces'
}
