import type { Control } from 'react-hook-form'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { WebSourceFormValues } from '../form-schema'

/** 编辑态不可变项的只读展示（spec §9.5.3：模式/入口地址/引擎/连接）。 */
export function ReadOnlyField({
  label,
  value,
}: {
  label: string
  value: string
}) {
  return (
    <div className='space-y-2'>
      <span className='font-medium text-sm'>{label}</span>
      <div className='rounded-lg border px-3 py-2 font-mono text-muted-foreground text-sm'>
        {value || '—'}
      </div>
    </div>
  )
}

// 数字字段在表单里以字符串承载（空输入合法），由 schema 校验、payload 构建转换。
export function NumberStringField({
  control,
  name,
  label,
}: {
  control: Control<WebSourceFormValues>
  name: 'max_pages' | 'max_depth'
  label: string
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input inputMode='numeric' {...field} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function SwitchField({
  control,
  name,
  label,
  description,
}: {
  control: Control<WebSourceFormValues>
  name:
    | 'allow_subdomains'
    | 'respect_robots'
    | 'fallback_to_full'
    | 'sync_enabled'
  label: string
  description?: string
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className='flex min-h-11 items-center justify-between gap-3 rounded-lg border px-3'>
          <div className='space-y-0.5'>
            <FormLabel>{label}</FormLabel>
            {description && <FormDescription>{description}</FormDescription>}
          </div>
          <FormControl>
            <Switch
              aria-label={label}
              checked={field.value}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )
}
