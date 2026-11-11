import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useId } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { ScrollArea } from '@/components/ui/scroll-area'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'
import { parseApiError } from '@/lib/api/error'
import { setMemberKBGrants } from '../api'
import { type MemberKBGrantsFormValues, memberKBGrantsSchema } from '../schemas'
import type { KBScope, Member } from '../types'

type MemberKBGrantsDialogProps = {
  workspaceSlug: string
  member: Member
  open: boolean
  onOpenChange: (open: boolean) => void
}

// MemberKBGrantsDialog 配置目标成员的知识库可见范围（all / restricted + 集合）。
// 后端约束仅对 member 角色生效；入口只在 member 行渲染（member-actions）。
export function MemberKBGrantsDialog({
  workspaceSlug,
  member,
  open,
  onOpenChange,
}: MemberKBGrantsDialogProps) {
  const { t } = useTranslation()
  const formId = useId()
  const queryClient = useQueryClient()
  const { data: knowledgeBases = [], isLoading } = useQuery({
    ...knowledgeBasesQueryOptions(workspaceSlug),
    enabled: open,
  })

  const form = useForm<MemberKBGrantsFormValues>({
    resolver: zodResolver(memberKBGrantsSchema),
    defaultValues: {
      kb_scope: member.kb_scope,
      knowledge_base_ids: member.knowledge_base_ids,
    },
  })
  const scope = form.watch('kb_scope')

  const mutation = useMutation({
    mutationFn: (values: MemberKBGrantsFormValues) =>
      setMemberKBGrants(workspaceSlug, member.user_id, {
        kb_scope: values.kb_scope,
        knowledge_base_ids:
          values.kb_scope === 'restricted'
            ? (values.knowledge_base_ids ?? [])
            : undefined,
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['members', workspaceSlug],
      })
      toast.success(t('members.actions.kbGrantsUpdatedToast'))
      onOpenChange(false)
    },
  })

  const toggleKnowledgeBase = (kbId: string, checked: boolean) => {
    const current = form.getValues('knowledge_base_ids') ?? []
    const next = checked
      ? [...current, kbId]
      : current.filter((id) => id !== kbId)
    form.setValue('knowledge_base_ids', next, { shouldDirty: true })
  }

  const handleSubmit = form.handleSubmit((values) => mutation.mutate(values))

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (!next) form.reset()
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('members.actions.kbGrantsDialogTitle')}</DialogTitle>
          <DialogDescription>
            {member.user?.email || member.user_id}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form id={formId} onSubmit={handleSubmit} className='space-y-4'>
            <FormField
              control={form.control}
              name='kb_scope'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('members.actions.kbGrantsScopeLabel')}
                  </FormLabel>
                  <FormControl>
                    <RadioGroup
                      value={field.value}
                      onValueChange={(value) =>
                        field.onChange(value as KBScope)
                      }
                      className='gap-3'
                    >
                      <Label
                        htmlFor='kb-grants-scope-all'
                        className='flex cursor-pointer items-start gap-2 rounded-md border p-3 font-normal'
                      >
                        <RadioGroupItem
                          value='all'
                          id='kb-grants-scope-all'
                          className='mt-0.5'
                        />
                        <span>
                          <span className='block font-medium text-sm'>
                            {t('members.actions.kbGrantsScopeAll')}
                          </span>
                          <span className='block text-muted-foreground text-xs'>
                            {t('members.actions.kbGrantsScopeAllHint')}
                          </span>
                        </span>
                      </Label>
                      <Label
                        htmlFor='kb-grants-scope-restricted'
                        className='flex cursor-pointer items-start gap-2 rounded-md border p-3 font-normal'
                      >
                        <RadioGroupItem
                          value='restricted'
                          id='kb-grants-scope-restricted'
                          className='mt-0.5'
                        />
                        <span>
                          <span className='block font-medium text-sm'>
                            {t('members.actions.kbGrantsScopeRestricted')}
                          </span>
                          <span className='block text-muted-foreground text-xs'>
                            {t('members.actions.kbGrantsScopeRestrictedHint')}
                          </span>
                        </span>
                      </Label>
                    </RadioGroup>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            {scope === 'restricted' && (
              <div className='space-y-2'>
                <div className='flex items-center justify-between text-muted-foreground text-xs'>
                  <span>
                    {t('members.actions.kbGrantsSelectedCount', {
                      selected: form.watch('knowledge_base_ids')?.length ?? 0,
                      total: knowledgeBases.length,
                    })}
                  </span>
                </div>
                {isLoading ? (
                  <div className='flex items-center gap-2 p-3 text-muted-foreground text-sm'>
                    <Loader2 className='size-4 animate-spin' />
                    {t('members.actions.kbGrantsLoading')}
                  </div>
                ) : (
                  <ScrollArea className='max-h-64 rounded-md border'>
                    <div className='p-1'>
                      {knowledgeBases.map((kb) => {
                        const checked =
                          form.watch('knowledge_base_ids')?.includes(kb.id) ??
                          false
                        const inputId = `kb-grants-item-${kb.id}`
                        return (
                          <Label
                            key={kb.id}
                            htmlFor={inputId}
                            className='flex cursor-pointer items-center gap-2 rounded-sm px-2 py-2 font-normal hover:bg-accent'
                          >
                            <Checkbox
                              id={inputId}
                              checked={checked}
                              onCheckedChange={(value) =>
                                toggleKnowledgeBase(kb.id, value === true)
                              }
                            />
                            <span className='text-sm'>{kb.name}</span>
                          </Label>
                        )
                      })}
                    </div>
                  </ScrollArea>
                )}
                {(form.watch('knowledge_base_ids')?.length ?? 0) === 0 && (
                  <p className='text-amber-600 text-xs dark:text-amber-400'>
                    {t('members.actions.kbGrantsEmptyWarning')}
                  </p>
                )}
              </div>
            )}

            {mutation.isError && (
              <p className='text-destructive text-sm' role='alert'>
                {parseApiError(mutation.error).message}
              </p>
            )}
          </form>
        </Form>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('members.actions.cancelButton')}
          </Button>
          <Button form={formId} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className='animate-spin' />}
            {t('members.actions.kbGrantsSaveButton')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
