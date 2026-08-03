import type { ComponentPropsWithoutRef, ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import rehypeSanitize from 'rehype-sanitize'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'

type SafeMarkdownProps = {
  content: string
  className?: string
}

function SafeLink({
  href,
  children,
  ...props
}: ComponentPropsWithoutRef<'a'> & { children?: ReactNode }) {
  if (!href) return <>{children}</>

  const external = /^https?:\/\//i.test(href)
  return (
    <a
      href={href}
      rel={external ? 'noreferrer noopener' : undefined}
      target={external ? '_blank' : undefined}
      {...props}
    >
      {children}
    </a>
  )
}

export function SafeMarkdown({ content, className }: SafeMarkdownProps) {
  return (
    <div
      className={cn(
        'space-y-3 break-words text-foreground text-sm leading-7',
        '[&_a]:font-medium [&_a]:text-primary [&_a]:underline [&_a]:underline-offset-4',
        '[&_blockquote]:border-border [&_blockquote]:border-l-2 [&_blockquote]:pl-4 [&_blockquote]:text-muted-foreground',
        '[&_code]:rounded-sm [&_code]:bg-muted [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.9em]',
        '[&_h1]:font-semibold [&_h1]:text-2xl [&_h2]:font-semibold [&_h2]:text-xl [&_h3]:font-semibold [&_h3]:text-lg',
        '[&_li]:ml-5 [&_ol]:list-decimal [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-muted [&_pre]:p-4 [&_table]:w-full [&_table]:border-collapse [&_td]:border [&_td]:border-border [&_td]:p-2 [&_th]:border [&_th]:border-border [&_th]:bg-muted/60 [&_th]:p-2 [&_th]:text-left [&_ul]:list-disc',
        className
      )}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeSanitize]}
        skipHtml
        components={{ a: SafeLink }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
