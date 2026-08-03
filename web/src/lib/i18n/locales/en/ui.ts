import type { ui as zhUi } from '../zh/ui'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

/**
 * shadcn/ui 基础组件的无障碍/可见文案（与 zh/ui.ts 结构一致）。
 */
export const ui = {
  close: 'Close',
  toggleSidebar: 'Toggle Sidebar',
  sidebarLabel: 'Sidebar',
  mobileSidebarDescription: 'Displays the mobile sidebar.',
} satisfies Widen<typeof zhUi>
