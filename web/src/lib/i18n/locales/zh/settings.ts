export const settings = {
  title: '设置',
  description: '调整管理台在当前浏览器中的显示偏好。',
  nav: {
    appearance: '外观',
    language: '语言',
  },
  appearance: {
    title: '主题',
    description: '侧栏始终保持深色，内容区域随主题切换。',
    pageDescription: '选择管理台的明暗主题。偏好只保存在当前浏览器中。',
    options: {
      light: '浅色',
      lightDescription: '深色侧栏与冷白内容画布',
      dark: '深色',
      darkDescription: '墨绿色画布与更深侧栏',
      system: '跟随系统',
      systemDescription: '自动匹配设备明暗设置',
    },
    submit: '保存主题',
    saved: '主题偏好已更新',
  },
  language: {
    title: '语言',
    description: '选择管理台界面的显示语言。',
    label: '界面语言',
    saved: '语言偏好已更新',
  },
} as const
