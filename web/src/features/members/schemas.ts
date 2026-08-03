import { z } from 'zod'
import i18n from '@/lib/i18n'

export const memberRoleSchema = z.object({
  role: z.enum(['owner', 'admin', 'member']),
})

export const passwordResetSchema = z
  .object({
    new_password: z
      .string()
      .min(8, { error: () => i18n.t('members.schemas.passwordMinLength') }),
    confirm_password: z.string().min(1, {
      error: () => i18n.t('members.schemas.confirmPasswordRequired'),
    }),
  })
  .refine((values) => values.new_password === values.confirm_password, {
    path: ['confirm_password'],
    error: () => i18n.t('members.schemas.passwordMismatch'),
  })

export type MemberRoleFormValues = z.infer<typeof memberRoleSchema>
export type PasswordResetFormValues = z.infer<typeof passwordResetSchema>
