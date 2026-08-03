import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, LogIn } from 'lucide-react'
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
import { login } from '@/features/auth/api'
import {
  chooseWorkspaceEntry,
  LAST_WORKSPACE_SLUG_KEY,
  safeRedirect,
} from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'
import { type LoginFormValues, loginSchema } from '@/features/auth/schemas'
import { parseApiError } from '@/lib/api/error'
import { resetUnauthorizedNavigation } from '@/lib/query-client'
import { cn } from '@/lib/utils'

interface UserAuthFormProps extends React.HTMLAttributes<HTMLFormElement> {
  redirectTo?: string
}

export function UserAuthForm({
  className,
  redirectTo,
  ...props
}: UserAuthFormProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: '', password: '' },
  })
  const loginMutation = useMutation({
    mutationFn: (input: LoginFormValues) => login(input),
    onSuccess: async () => {
      resetUnauthorizedNavigation()
      const me = await queryClient.fetchQuery(meQueryOptions())
      const recentSlug = localStorage.getItem(LAST_WORKSPACE_SLUG_KEY)
      const target =
        safeRedirect(redirectTo) ??
        chooseWorkspaceEntry(me, recentSlug ?? undefined)
      toast.success(t('auth.signIn.successToast'))
      await navigate({ to: target, replace: true })
    },
  })

  const errorMessage = loginMutation.isError
    ? parseApiError(loginMutation.error).status === 429
      ? t('auth.signIn.rateLimited')
      : parseApiError(loginMutation.error).message
    : undefined

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) => loginMutation.mutate(values))}
        className={cn('grid gap-4', className)}
        {...props}
      >
        <FormField
          control={form.control}
          name='email'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('auth.signIn.emailLabel')}</FormLabel>
              <FormControl>
                <Input
                  autoComplete='email'
                  inputMode='email'
                  placeholder='name@example.com'
                  {...field}
                />
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
              <FormLabel>{t('auth.signIn.passwordLabel')}</FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='current-password'
                  placeholder={t('auth.signIn.passwordPlaceholder')}
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
        <Button className='mt-1' disabled={loginMutation.isPending}>
          {loginMutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <LogIn />
          )}
          {t('auth.signIn.submitButton')}
        </Button>
      </form>
    </Form>
  )
}
