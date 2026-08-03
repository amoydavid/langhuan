import { z } from 'zod'
import i18n from '@/lib/i18n'

const passwordSchema = z
  .string()
  .min(1, { error: () => i18n.t('auth.schemas.passwordRequired') })
  .min(8, { error: () => i18n.t('auth.schemas.passwordMinLength') })

export const loginSchema = z.object({
  email: z.email({ error: () => i18n.t('auth.schemas.invalidEmail') }),
  password: passwordSchema,
})

const registrationFields = {
  nickname: z
    .string()
    .trim()
    .min(1, { error: () => i18n.t('auth.schemas.nicknameRequired') }),
  password: passwordSchema,
  confirm_password: z
    .string()
    .min(1, { error: () => i18n.t('auth.schemas.confirmPasswordRequired') }),
}

export const setupSchema = z
  .object({
    email: z.email({ error: () => i18n.t('auth.schemas.invalidEmail') }),
    ...registrationFields,
  })
  .refine((data) => data.password === data.confirm_password, {
    error: () => i18n.t('auth.schemas.passwordMismatch'),
    path: ['confirm_password'],
  })

export const invitationRegistrationSchema = z
  .object(registrationFields)
  .refine((data) => data.password === data.confirm_password, {
    error: () => i18n.t('auth.schemas.passwordMismatch'),
    path: ['confirm_password'],
  })

export type LoginFormValues = z.infer<typeof loginSchema>
export type SetupFormValues = z.infer<typeof setupSchema>
export type InvitationRegistrationFormValues = z.infer<
  typeof invitationRegistrationSchema
>
