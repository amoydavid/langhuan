import { Check, Copy, Eye, EyeOff, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { parseApiError } from '@/lib/api/error'
import { useRevealAPIKey } from '../queries'

type APIKeySecretPanelProps = {
  workspaceSlug: string
  apiKeyId: string
  // initialSecret 来自创建响应的一次性明文（可选）。一旦传入，由组件本地 state 接管。
  initialSecret?: string
  // revealDisabled 表示后端已不可 reveal（已吊销/已过期等）。
  revealDisabled?: boolean
}

// 安全约束：明文只存在于本组件的 useState 中，
// 绝不写入 TanStack Query 缓存、URL、Zustand、localStorage 或 sessionStorage。
// apiKeyId 变化或卸载时立即清空。
export function APIKeySecretPanel({
  workspaceSlug,
  apiKeyId,
  initialSecret,
  revealDisabled,
}: APIKeySecretPanelProps) {
  const { t } = useTranslation()
  // 明文仅存活于组件本地 state。调用方通过 React key 在 apiKeyId 变化时
  // 整体重新挂载本组件，从而清空明文；这里只需保证卸载时再次清空。
  const [secret, setSecret] = useState<string | null>(initialSecret ?? null)
  const [revealed, setRevealed] = useState(false)
  const [copied, setCopied] = useState(false)

  // 卸载时清空，确保明文引用随组件销毁释放。
  useEffect(() => {
    return () => {
      setSecret(null)
      setRevealed(false)
      setCopied(false)
    }
  }, [])

  // 通过 useRevealAPIKey 获取一次性明文，写入本地 state，绝不进入 query cache。
  const reveal = useRevealAPIKey(workspaceSlug)
  const handleReveal = () => {
    reveal.mutate(apiKeyId, {
      onSuccess: (data) => {
        setSecret(data.api_key)
        setRevealed(true)
        setCopied(false)
      },
    })
  }

  async function handleCopy() {
    if (!secret) return
    try {
      await navigator.clipboard.writeText(secret)
      setCopied(true)
      toast.success(t('apiKeys.secretPanel.copiedToast'))
    } catch {
      toast.error(t('apiKeys.secretPanel.copyFailedToast'))
    }
  }

  if (!secret) {
    return (
      <div className='space-y-2'>
        <Button
          type='button'
          variant='outline'
          onClick={() => handleReveal()}
          disabled={revealDisabled || reveal.isPending}
        >
          {reveal.isPending ? <Loader2 className='animate-spin' /> : <Eye />}
          {t('apiKeys.secretPanel.reveal')}
        </Button>
        {reveal.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(reveal.error).message}
          </p>
        )}
        <p className='text-muted-foreground text-xs'>
          {t('apiKeys.secretPanel.revealHint')}
        </p>
      </div>
    )
  }

  return (
    <div className='space-y-2'>
      <code className='block break-all rounded-lg border bg-muted/50 p-3 font-mono text-sm'>
        {revealed ? secret : '•'.repeat(Math.min(secret.length, 32))}
      </code>
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          variant='outline'
          onClick={() => setRevealed((v) => !v)}
        >
          {revealed ? <EyeOff /> : <Eye />}
          {revealed
            ? t('apiKeys.secretPanel.hide')
            : t('apiKeys.secretPanel.show')}
        </Button>
        <Button
          type='button'
          variant='outline'
          onClick={() => void handleCopy()}
        >
          {copied ? <Check /> : <Copy />}
          {copied
            ? t('apiKeys.secretPanel.copied')
            : t('apiKeys.secretPanel.copy')}
        </Button>
        <Button
          type='button'
          variant='ghost'
          onClick={() => {
            setSecret(null)
            setRevealed(false)
            setCopied(false)
          }}
        >
          {t('apiKeys.secretPanel.clear')}
        </Button>
      </div>
    </div>
  )
}
