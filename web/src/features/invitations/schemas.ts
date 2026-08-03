import { z } from 'zod'
import i18n from '@/lib/i18n'

export const invitationSchema = z.object({
  invited_email: z.email({
    error: () => i18n.t('invitations.schemas.invalidEmail'),
  }),
  role: z.enum(['owner', 'admin', 'member']),
})

export type InvitationFormValues = z.infer<typeof invitationSchema>
