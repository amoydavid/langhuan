import { z } from 'zod'
import i18n from '@/lib/i18n'
import {
  optionalURLSchema,
  providerCommonFields,
  timeoutSchema,
} from './common'

export const openAIProviderSchema = z
  .object({
    ...providerCommonFields,
    provider: z.literal('openai'),
    mode: z.enum(['standard', 'azure']),
    base_url: optionalURLSchema,
    api_version: z.string().trim(),
    timeout_seconds: timeoutSchema,
    api_key: z.string(),
    custom_headers: z.string().superRefine((value, context) => {
      try {
        parseCustomHeadersText(value)
      } catch (error) {
        context.addIssue({
          code: 'custom',
          message:
            error instanceof Error
              ? error.message
              : i18n.t('models.schemas.customHeadersInvalid'),
        })
      }
    }),
  })
  .strict()

export function parseCustomHeadersText(input: string) {
  const headers: Record<string, string> = {}
  const names = new Set<string>()
  for (const [index, source] of input.split(/\r?\n/).entries()) {
    const line = source.trim()
    if (!line) continue
    const separator = line.indexOf(':')
    const name = line.slice(0, separator).trim()
    const value = line.slice(separator + 1).trim()
    if (separator < 1 || !name || !value) {
      throw new Error(
        i18n.t('models.schemas.headerLineFormat', { line: index + 1 })
      )
    }
    const normalized = name.toLowerCase()
    if (names.has(normalized)) {
      throw new Error(
        i18n.t('models.schemas.headerNameDuplicate', { line: index + 1 })
      )
    }
    names.add(normalized)
    headers[name] = value
  }
  return headers
}
