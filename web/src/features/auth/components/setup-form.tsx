import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, UserRoundCheck } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Input } from '@/components/ui/input'
import { registerUser } from '@/features/auth/api'
import { bootstrapStatusQueryOptions } from '@/features/auth/queries'
import { type SetupFormValues, setupSchema } from '@/features/auth/schemas'
import { parseApiError } from '@/lib/api/error'
import { cn } from '@/lib/utils'

export function SetupForm({
  className,
  ...props
}: React.HTMLAttributes<HTMLFormElement>) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const form = useForm<SetupFormValues>({
    resolver: zodResolver(setupSchema),
    defaultValues: {
      email: '',
      nickname: '',
      password: '',
      confirm_password: '',
    },
  })
  const mutation = useMutation({
    mutationFn: (input: Parameters<typeof registerUser>[0]) =>
      registerUser(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['bootstrap-status'] })
      await queryClient.fetchQuery(bootstrapStatusQueryOptions())
      toast.success(t('auth.setup.successToast'))
      await navigate({ to: '/sign-in', replace: true })
    },
  })

  function submit(values: SetupFormValues) {
    mutation.mutate({
      email: values.email,
      nickname: values.nickname,
      password: values.password,
    })
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(submit)}
        className={cn('grid gap-4', className)}
        {...props}
      >
        <FormField
          control={form.control}
          name='email'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('auth.setup.emailLabel')}</FormLabel>
              <FormControl>
                <Input autoComplete='email' inputMode='email' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='nickname'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('auth.setup.nicknameLabel')}</FormLabel>
              <FormControl>
                <Input autoComplete='name' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('auth.setup.passwordLabel')}</FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
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
              <FormLabel>{t('auth.setup.confirmPasswordLabel')}</FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        {mutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(mutation.error).message}
          </p>
        )}
        <Button className='mt-1' disabled={mutation.isPending}>
          {mutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <UserRoundCheck />
          )}
          {t('auth.setup.submitButton')}
        </Button>
      </form>
    </Form>
  )
}
