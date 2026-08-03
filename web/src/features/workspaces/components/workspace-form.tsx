import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, Plus } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { parseApiError } from '@/lib/api/error'
import { createWorkspace } from '../api'
import { type WorkspaceFormValues, workspaceSchema } from '../schemas'

type WorkspaceFormProps = {
  isPlatformAdmin: boolean
}

export function WorkspaceForm({ isPlatformAdmin }: WorkspaceFormProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const form = useForm<WorkspaceFormValues>({
    resolver: zodResolver(workspaceSchema),
    defaultValues: { name: '', slug: '' },
  })
  const mutation = useMutation({
    mutationFn: (input: Parameters<typeof createWorkspace>[0]) =>
      createWorkspace(input),
    onSuccess: async (workspace) => {
      await queryClient.invalidateQueries({ queryKey: ['me'] })
      toast.success(t('workspaces.form.createdToast'))
      await navigate({
        to: `/workspaces/${encodeURIComponent(workspace.slug)}/kb`,
        replace: true,
      })
    },
  })

  if (!isPlatformAdmin) {
    return (
      <Alert>
        <AlertTitle>{t('workspaces.form.adminOnlyTitle')}</AlertTitle>
        <AlertDescription>
          {t('workspaces.form.adminOnlyDescription')}
        </AlertDescription>
      </Alert>
    )
  }

  function submit(values: WorkspaceFormValues) {
    mutation.mutate({
      name: values.name,
      slug: values.slug,
    })
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(submit)} className='grid gap-5'>
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('workspaces.form.nameLabel')}</FormLabel>
              <FormControl>
                <Input autoComplete='organization' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='slug'
          render={({ field }) => (
            <FormItem>
              <FormLabel>Slug</FormLabel>
              <FormControl>
                <Input autoCapitalize='none' autoComplete='off' {...field} />
              </FormControl>
              <FormDescription>
                {t('workspaces.form.slugDescription')}
              </FormDescription>
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
          {mutation.isPending ? <Loader2 className='animate-spin' /> : <Plus />}
          {t('workspaces.form.createButton')}
        </Button>
      </form>
    </Form>
  )
}
