import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const statusBadgeVariants = cva(
  'inline-flex w-fit shrink-0 items-center gap-1.5 rounded-md border px-2 py-0.5 font-medium text-xs',
  {
    variants: {
      tone: {
        success: 'border-success/20 bg-success/10 text-success',
        warning: 'border-warning/20 bg-warning/10 text-warning',
        danger: 'border-danger/20 bg-danger/10 text-danger',
        info: 'border-info/20 bg-info/10 text-info',
        neutral: 'border-border bg-secondary text-muted-foreground',
      },
    },
    defaultVariants: { tone: 'neutral' },
  }
)

type StatusBadgeProps = React.ComponentProps<'span'> &
  VariantProps<typeof statusBadgeVariants>

export function StatusBadge({
  children,
  className,
  tone,
  ...props
}: StatusBadgeProps) {
  return (
    <span
      data-slot='status-badge'
      data-tone={tone ?? 'neutral'}
      className={cn(statusBadgeVariants({ tone }), className)}
      {...props}
    >
      <span
        data-slot='status-indicator'
        className='size-1.5 rounded-full bg-current'
        aria-hidden='true'
      />
      {children}
    </span>
  )
}

export { statusBadgeVariants }
