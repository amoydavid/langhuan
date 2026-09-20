import type { SourceProvider } from './types'

/** provider 稳定序列：表单单选与卡片徽标共用，保证展示顺序一致。 */
export const sourceProviders: readonly SourceProvider[] = [
  'feishu',
  'firecrawl',
  'jina',
  'browser',
]

/** 支持连接测试的 provider（后端契约：feishu 不支持测试，测试端点返回 400）。 */
export const testableProviders: ReadonlySet<SourceProvider> = new Set([
  'firecrawl',
  'jina',
  'browser',
])

// i18n key 保持字面量类型，供严格类型的 t() 校验（zh/en 结构一致由 satisfies 保证）。

const providerBadgeKeys = {
  feishu: 'integrations.list.providerFeishu',
  firecrawl: 'integrations.list.providerFirecrawl',
  jina: 'integrations.list.providerJina',
  browser: 'integrations.list.providerBrowser',
} as const satisfies Record<SourceProvider, string>

/** provider 在连接卡片上的徽标文案 key。 */
export function sourceProviderBadgeKey(provider: SourceProvider) {
  return providerBadgeKeys[provider]
}

const secretLabelKeys = {
  feishu: 'integrations.form.appSecretLabel',
  firecrawl: 'integrations.form.apiKeyLabel',
  jina: 'integrations.form.apiKeyLabel',
  browser: 'integrations.form.tokenLabel',
} as const satisfies Record<SourceProvider, string>

/** provider 对应的凭证字段文案 key（feishu=App Secret、firecrawl/jina=API Key、browser=Token）。 */
export function secretFieldLabelKey(provider: SourceProvider) {
  return secretLabelKeys[provider]
}

const secretPlaceholderKeys = {
  feishu: 'integrations.form.appSecretPlaceholder',
  firecrawl: 'integrations.form.apiKeyPlaceholder',
  jina: 'integrations.form.apiKeyPlaceholder',
  browser: 'integrations.form.tokenPlaceholder',
} as const satisfies Record<SourceProvider, string>

/** provider 对应的凭证输入占位文案 key。 */
export function secretFieldPlaceholderKey(provider: SourceProvider) {
  return secretPlaceholderKeys[provider]
}
