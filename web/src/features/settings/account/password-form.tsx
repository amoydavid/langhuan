import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { changePassword } from '@/features/auth/api'
import { parseApiError } from '@/lib/api/error'

const passwordSchema = z
  .object({
    old_password: z.string().min(1),
    new_password: z.string().min(8),
    confirm_password: z.string().min(1),
  })
  .refine((data) => data.new_password === data.confirm_password, {
    path: ['confirm_password'],
  })

type PasswordFormValues = z.infer<typeof passwordSchema>

export function PasswordForm() {
  const { t } = useTranslation()
  const form = useForm<PasswordFormValues>({
    resolver: zodResolver(passwordSchema),
    defaultValues: { old_password: '', new_password: '', confirm_password: '' },
  })

  const mutation = useMutation({
    mutationFn: (values: PasswordFormValues) =>
      changePassword({
        old_password: values.old_password,
        new_password: values.new_password,
      }),
    onSuccess: () => {
      toast.success(t('settings.account.passwordChanged'))
      form.reset()
    },
  })

  const errorMessage = mutation.isError
    ? parseApiError(mutation.error).status === 401
      ? t('settings.account.wrongOldPassword')
      : parseApiError(mutation.error).message
    : undefined

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        className='space-y-4'
      >
        <FormField
          control={form.control}
          name='old_password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('settings.account.oldPassword')}</FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='current-password'
                  placeholder={t('settings.account.oldPasswordPlaceholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='new_password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('settings.account.newPassword')}</FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='new-password'
                  placeholder={t('settings.account.newPasswordPlaceholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='confirm_password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('settings.account.confirmPassword')}</FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='new-password'
                  placeholder={t('settings.account.confirmPasswordPlaceholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        {errorMessage && (
          <p className='text-destructive text-sm' role='alert'>
            {errorMessage}
          </p>
        )}
        <Button type='submit' disabled={mutation.isPending}>
          {mutation.isPending ? <Loader2 className='animate-spin' /> : null}
          {t('settings.account.changePasswordSubmit')}
        </Button>
      </form>
    </Form>
  )
}
