import { cva, type VariantProps } from 'class-variance-authority'
import type { TFunction } from 'i18next'
import type { APIKeyScope, APIKeyStatus } from './types'

// 用一个本地 cva 定义推导合法 Badge 变体类型，与 ui/badge 的变体集合保持同步，
// 同时避免把组件实现细节引入纯数据文件。
const badgeVariantRefs = cva('', {
  variants: {
    variant: {
      default: '',
      secondary: '',
      destructive: '',
      outline: '',
    },
  },
})

type BadgeVariant = NonNullable<
  VariantProps<typeof badgeVariantRefs>['variant']
>

// 后端状态到中文标签的映射；文案随当前语言惰性求值（语言切换后即时生效）。
export function apiKeyStatusLabel(t: TFunction): Record<APIKeyStatus, string> {
  return {
    active: t('apiKeys.display.statusActive'),
    expiring: t('apiKeys.display.statusExpiring'),
    expired: t('apiKeys.display.statusExpired'),
    revoked: t('apiKeys.display.statusRevoked'),
  }
}

// 状态对应 Badge 变体，便于一眼区分。
export const apiKeyStatusBadgeVariant: Record<APIKeyStatus, BadgeVariant> = {
  active: 'secondary',
  expiring: 'outline',
  expired: 'destructive',
  revoked: 'destructive',
}

// scope 到中文标签的映射；文案随当前语言惰性求值。
export function apiKeyScopeLabel(t: TFunction): Record<APIKeyScope, string> {
  return {
    'knowledge_bases:write': t('apiKeys.display.scopeKnowledgeBasesWrite'),
    'documents:read': t('apiKeys.display.scopeDocumentsRead'),
    'documents:write': t('apiKeys.display.scopeDocumentsWrite'),
    'search:read': t('apiKeys.display.scopeSearchRead'),
  }
}

// scope 顺序固定，与后端 AllAPIScopes 一致，便于展示稳定。
export const apiKeyScopeOrder: readonly APIKeyScope[] = [
  'knowledge_bases:write',
  'documents:read',
  'documents:write',
  'search:read',
] as const
