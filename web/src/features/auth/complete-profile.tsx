import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, Mail } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { parseApiError } from '@/lib/api/error'
import { resetUnauthorizedNavigation } from '@/lib/query-client'
import { Route } from '@/routes/(auth)/complete-profile'
import { updateProfile } from './api'
import { AuthLayout } from './auth-layout'

const completeProfileSchema = z.object({
  email: z.string().min(1).email(),
})

type CompleteProfileValues = z.infer<typeof completeProfileSchema>

/**
 * 补齐资料页：OIDC IdP 未返回 email 时，登录后引导用户在这里补充 email。
 * 可选携带 invitation_token_hash（补齐 email 后完成此前待接受的邀请）。
 */
export function CompleteProfile() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const search = Route.useSearch()

  const form = useForm<CompleteProfileValues>({
    resolver: zodResolver(completeProfileSchema),
    defaultValues: { email: '' },
  })

  const mutation = useMutation({
    mutationFn: (values: CompleteProfileValues) =>
      updateProfile({
        email: values.email,
        invitation_token_hash: search.invitation_token_hash,
      }),
    onSuccess: async () => {
      resetUnauthorizedNavigation()
      await queryClient.invalidateQueries({ queryKey: ['me'] })
      toast.success(t('auth.completeProfile.success'))
      const nextPath = search.next ?? '/workspaces'
      // 补齐资料后全页跳转（next 来自服务端校验过的站内路径）。
      window.location.assign(nextPath)
    },
  })

  const errorMessage = mutation.isError
    ? parseApiError(mutation.error).status === 409
      ? t('auth.completeProfile.emailTaken')
      : parseApiError(mutation.error).status === 403
        ? t('auth.completeProfile.emailMismatch')
        : parseApiError(mutation.error).message
    : undefined

  return (
    <AuthLayout>
      <Card className='w-full max-w-sm gap-5 border-border-strong/70 bg-card/95'>
        <CardHeader>
          <CardTitle className='text-xl tracking-tight'>
            {t('auth.completeProfile.title')}
          </CardTitle>
          <CardDescription>
            {t('auth.completeProfile.description')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
              className='grid gap-4'
            >
              <FormField
                control={form.control}
                name='email'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('auth.completeProfile.emailLabel')}
                    </FormLabel>
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
              {errorMessage && (
                <p className='text-destructive text-sm' role='alert'>
                  {errorMessage}
                </p>
              )}
              <Button className='mt-1' disabled={mutation.isPending}>
                {mutation.isPending ? (
                  <Loader2 className='animate-spin' />
                ) : (
                  <Mail />
                )}
                {t('auth.completeProfile.submit')}
              </Button>
            </form>
          </Form>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
