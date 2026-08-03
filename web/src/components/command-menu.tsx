import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import {
  ArrowRight,
  BookOpen,
  Laptop,
  MessageSquarePlus,
  Moon,
  Plus,
  Search,
  Sun,
  Upload,
} from 'lucide-react'
import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  buildPlatformNavigation,
  buildWorkspaceNavigation,
} from '@/components/layout/workspace-navigation'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useSearch } from '@/context/search-provider'
import { useTheme } from '@/context/theme-provider'
import { workspaceEntry } from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'

export function CommandMenu() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = useParams({ strict: false }) as {
    workspaceSlug?: string
    kbId?: string
  }
  const { data: me } = useQuery(meQueryOptions())
  const workspaceSlug = params.workspaceSlug ?? ''
  const { data: knowledgeBases = [] } = useQuery({
    ...knowledgeBasesQueryOptions(workspaceSlug),
    enabled: workspaceSlug.length > 0,
  })
  const { setTheme } = useTheme()
  const { open, setOpen } = useSearch()
  const currentWorkspace = me?.workspaces.find(
    (workspace) => workspace.slug === params.workspaceSlug
  )
  const navGroups = currentWorkspace
    ? buildWorkspaceNavigation(
        currentWorkspace.slug,
        currentWorkspace.role,
        me?.user.is_platform_admin ?? false
      )
    : buildPlatformNavigation(me?.user.is_platform_admin ?? false)
  const currentKnowledgeBase = knowledgeBases.find(
    (knowledgeBase) => knowledgeBase.id === params.kbId
  )

  const runCommand = React.useCallback(
    (command: () => unknown) => {
      setOpen(false)
      command()
    },
    [setOpen]
  )

  return (
    <CommandDialog modal open={open} onOpenChange={setOpen}>
      <CommandInput placeholder={t('common.commandMenu.searchPlaceholder')} />
      <CommandList>
        <ScrollArea type='hover' className='h-72 pe-1'>
          <CommandEmpty>{t('common.commandMenu.noResults')}</CommandEmpty>
          {navGroups.map((group) => (
            <CommandGroup key={group.title} heading={group.title}>
              {group.items.flatMap((item) =>
                'url' in item && item.url
                  ? [
                      <CommandItem
                        key={`${item.title}-${item.url}`}
                        value={item.title}
                        onSelect={() =>
                          runCommand(() => navigate({ to: item.url }))
                        }
                      >
                        <ArrowRight className='size-4 text-muted-foreground' />
                        {item.title}
                      </CommandItem>,
                    ]
                  : []
              )}
            </CommandGroup>
          ))}
          {!!me?.workspaces.length && (
            <CommandGroup heading='Workspaces'>
              {me.workspaces.map((workspace) => (
                <CommandItem
                  key={workspace.workspace_id}
                  value={`workspace-${workspace.name}-${workspace.slug}`}
                  onSelect={() =>
                    runCommand(() =>
                      navigate({ to: workspaceEntry(workspace.slug) })
                    )
                  }
                >
                  <BookOpen className='size-4' />
                  {workspace.name}
                </CommandItem>
              ))}
            </CommandGroup>
          )}
          {!!knowledgeBases.length && workspaceSlug && (
            <CommandGroup heading={t('common.commandMenu.knowledgeBases')}>
              {knowledgeBases.map((knowledgeBase) => (
                <CommandItem
                  key={knowledgeBase.id}
                  value={`kb-${knowledgeBase.name}`}
                  onSelect={() =>
                    runCommand(() =>
                      navigate({
                        to: `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(knowledgeBase.id)}`,
                      })
                    )
                  }
                >
                  <BookOpen className='size-4' />
                  {knowledgeBase.name}
                </CommandItem>
              ))}
            </CommandGroup>
          )}
          {currentWorkspace && (
            <CommandGroup heading={t('common.commandMenu.quickActions')}>
              <CommandItem
                value={t('common.commandMenu.createKnowledgeBase')}
                onSelect={() =>
                  runCommand(() =>
                    navigate({
                      to: '/workspaces/$workspaceSlug/kb/new',
                      params: { workspaceSlug: currentWorkspace.slug },
                    })
                  )
                }
              >
                <Plus className='size-4' />
                {t('common.commandMenu.createKnowledgeBase')}
              </CommandItem>
              {currentKnowledgeBase && (
                <>
                  <CommandItem
                    value={`${t('common.commandMenu.uploadFileToKb', { name: currentKnowledgeBase.name })}`}
                    onSelect={() =>
                      runCommand(() =>
                        navigate({
                          to: '/workspaces/$workspaceSlug/kb/$kbId/documents/new',
                          params: {
                            workspaceSlug: currentWorkspace.slug,
                            kbId: currentKnowledgeBase.id,
                          },
                        })
                      )
                    }
                  >
                    <Upload className='size-4' />
                    {t('common.commandMenu.uploadFileToKb', {
                      name: currentKnowledgeBase.name,
                    })}
                  </CommandItem>
                  <CommandItem
                    value={`${t('common.commandMenu.createFaq')}-${currentKnowledgeBase.name}`}
                    onSelect={() =>
                      runCommand(() =>
                        navigate({
                          href: `/workspaces/${encodeURIComponent(currentWorkspace.slug)}/kb/${encodeURIComponent(currentKnowledgeBase.id)}/content/faq/new`,
                        })
                      )
                    }
                  >
                    <MessageSquarePlus className='size-4' />
                    {t('common.commandMenu.createFaq')}
                  </CommandItem>
                  <CommandItem
                    value={`${t('common.commandMenu.openSearchTest')}-${currentKnowledgeBase.name}`}
                    onSelect={() =>
                      runCommand(() =>
                        navigate({
                          href: `/workspaces/${encodeURIComponent(currentWorkspace.slug)}/kb/${encodeURIComponent(currentKnowledgeBase.id)}/search`,
                        })
                      )
                    }
                  >
                    <Search className='size-4' />
                    {t('common.commandMenu.openSearchTest')}
                  </CommandItem>
                </>
              )}
            </CommandGroup>
          )}
          <CommandSeparator />
          <CommandGroup heading={t('common.commandMenu.theme')}>
            <CommandItem onSelect={() => runCommand(() => setTheme('light'))}>
              <Sun /> <span>{t('common.commandMenu.light')}</span>
            </CommandItem>
            <CommandItem onSelect={() => runCommand(() => setTheme('dark'))}>
              <Moon className='scale-90' />
              <span>{t('common.commandMenu.dark')}</span>
            </CommandItem>
            <CommandItem onSelect={() => runCommand(() => setTheme('system'))}>
              <Laptop />
              <span>{t('common.commandMenu.system')}</span>
            </CommandItem>
          </CommandGroup>
        </ScrollArea>
      </CommandList>
    </CommandDialog>
  )
}
