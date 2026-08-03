import { z } from 'zod'
import i18n from '@/lib/i18n'

export const workspaceSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, { error: () => i18n.t('workspaces.form.nameRequired') }),
  slug: z
    .string()
    .trim()
    .min(3, { error: () => i18n.t('workspaces.form.slugMinLength') })
    .max(64, { error: () => i18n.t('workspaces.form.slugMaxLength') })
    .regex(/^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$/, {
      error: () => i18n.t('workspaces.form.slugFormat'),
    }),
})

export type WorkspaceFormValues = z.infer<typeof workspaceSchema>
