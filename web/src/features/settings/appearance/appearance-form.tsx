import { zodResolver } from '@hookform/resolvers/zod'
import { Monitor, Moon, Sun } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
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
import { useTheme } from '@/context/theme-provider'
import { cn } from '@/lib/utils'

const appearanceFormSchema = z.object({
  theme: z.enum(['light', 'dark', 'system']),
})

type AppearanceFormValues = z.infer<typeof appearanceFormSchema>

export function AppearanceForm() {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()
  const form = useForm<AppearanceFormValues>({
    resolver: zodResolver(appearanceFormSchema),
    defaultValues: { theme },
  })

  const themeOptions = [
    {
      value: 'light',
      label: t('settings.appearance.options.light'),
      description: t('settings.appearance.options.lightDescription'),
      icon: Sun,
      preview: 'bg-preview-light-canvas',
      sidebar: 'bg-preview-light-sidebar',
      panel: 'bg-preview-light-panel',
    },
    {
      value: 'dark',
      label: t('settings.appearance.options.dark'),
      description: t('settings.appearance.options.darkDescription'),
      icon: Moon,
      preview: 'bg-preview-dark-canvas',
      sidebar: 'bg-preview-dark-sidebar',
      panel: 'bg-preview-dark-panel',
    },
    {
      value: 'system',
      label: t('settings.appearance.options.system'),
      description: t('settings.appearance.options.systemDescription'),
      icon: Monitor,
      preview: 'bg-secondary',
      sidebar: 'bg-preview-system-sidebar',
      panel: 'bg-primary/20',
    },
  ] as const

  function onSubmit(data: AppearanceFormValues) {
    setTheme(data.theme)
    toast.success(t('settings.appearance.saved'))
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
        <FormField
          control={form.control}
          name='theme'
          render={({ field }) => (
            <FormItem>
              <div className='mb-3'>
                <FormLabel>{t('settings.appearance.title')}</FormLabel>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t('settings.appearance.description')}
                </p>
              </div>
              <FormControl>
                <RadioGroup
                  value={field.value}
                  onValueChange={field.onChange}
                  className='grid gap-3 sm:grid-cols-3'
                >
                  {themeOptions.map((option) => (
                    <Label
                      key={option.value}
                      className='group cursor-pointer font-normal'
                    >
                      <RadioGroupItem
                        value={option.value}
                        aria-label={option.label}
                        className='sr-only'
                      />
                      <div className='rounded-[10px] border border-border bg-card p-3 transition-colors group-hover:border-border-strong group-has-data-[state=checked]:border-primary group-has-data-[state=checked]:ring-2 group-has-data-[state=checked]:ring-primary/15'>
                        <div
                          className={cn(
                            'mb-3 flex h-24 overflow-hidden rounded-md border',
                            option.preview
                          )}
                        >
                          <div className={cn('w-1/4 p-2', option.sidebar)}>
                            <div className='mb-2 h-2 w-5 rounded-full bg-primary' />
                            <div className='space-y-1.5 opacity-70'>
                              <div className='h-1.5 rounded-full bg-white/50' />
                              <div className='h-1.5 rounded-full bg-white/20' />
                              <div className='h-1.5 rounded-full bg-white/20' />
                            </div>
                          </div>
                          <div className='flex-1 p-2.5'>
                            <div className='mb-2 h-2 w-2/5 rounded-full bg-primary' />
                            <div
                              className={cn(
                                'h-12 rounded-md border border-white/10',
                                option.panel
                              )}
                            />
                          </div>
                        </div>
                        <div className='flex items-start gap-2'>
                          <option.icon className='mt-0.5 size-4 text-primary' />
                          <div>
                            <div className='font-medium text-sm'>
                              {option.label}
                            </div>
                            <div className='mt-0.5 text-muted-foreground text-xs leading-4'>
                              {option.description}
                            </div>
                          </div>
                        </div>
                      </div>
                    </Label>
                  ))}
                </RadioGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <Button type='submit'>{t('settings.appearance.submit')}</Button>
      </form>
    </Form>
  )
}
