import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { KeyRound, Loader2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { parseApiError } from '@/lib/api/error'
import { createSourceConnectionMutationOptions } from '../queries'
import { createSourceConnectionSchema } from '../schemas'
import type { CreateSourceConnectionInput, SourceConnection } from '../types'

// 表单值直接复用 create schema 的输出，包含 provider 字段，
// 使 zodResolver 类型与 RHF useForm 完全对齐。
type SourceConnectionFormValues = CreateSourceConnectionInput

type SourceConnectionFormProps = {
  workspaceSlug: string
  // 编辑模式预留：首版仅实现 create。
  connection?: SourceConnection
}

export function SourceConnectionForm({
  workspaceSlug,
}: SourceConnectionFormProps) {
  // TODO: 编辑模式（connection 不为空时）暂未实现，首版仅支持 create。
  // 后续如需编辑可在此分支复用 zodResolver(updateSourceConnectionSchema)。
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { t } = useTranslation()

  const form = useForm<SourceConnectionFormValues>({
    resolver: zodResolver(createSourceConnectionSchema),
    defaultValues: {
      provider: 'feishu',
      name: '',
      app_id: '',
      app_secret: '',
    },
  })

  const mutation = useMutation(
    createSourceConnectionMutationOptions(workspaceSlug)
  )

  async function onSubmit(values: SourceConnectionFormValues) {
    // TODO: 在保存前调用飞书 token 接口做一次连通性测试，验证 App ID/Secret。
    try {
      await mutation.mutateAsync(values)
      await queryClient.invalidateQueries({
        queryKey: ['source-connections', workspaceSlug],
      })
      toast.success(t('integrations.form.createdToast'))
      await navigate({
        to: `/workspaces/${encodeURIComponent(workspaceSlug)}/integrations`,
        replace: true,
      })
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='grid gap-5'>
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('integrations.form.nameLabel')}</FormLabel>
              <FormControl>
                <Input
                  autoFocus
                  placeholder={t('integrations.form.namePlaceholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='app_id'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('integrations.form.appIdLabel')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('integrations.form.appIdPlaceholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='app_secret'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('integrations.form.appSecretLabel')}</FormLabel>
              <FormControl>
                <Input
                  type='password'
                  autoComplete='new-password'
                  placeholder={t('integrations.form.appSecretPlaceholder')}
                  {...field}
                />
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
        <Button className='w-fit' disabled={mutation.isPending}>
          {mutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <KeyRound />
          )}
          {t('integrations.form.submitButton')}
        </Button>
      </form>
    </Form>
  )
}
