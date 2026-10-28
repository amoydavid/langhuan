import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

type FAQInsightHintProps = {
  /** 健康面板零召回查询原文；缺失（普通新建）时不渲染提示条。 */
  question?: string
}

/**
 * 来源提示条（v1.4.0 spec §9.3.6）：仅在 `content/faq/new?question=…`
 * 预填进入时渲染，说明本页来自健康面板的零召回查询。
 */
export function FAQInsightHint({ question }: FAQInsightHintProps) {
  const { t } = useTranslation()
  if (!question) return null
  return (
    <div
      data-testid='faq-insight-hint'
      className='flex items-start gap-2 rounded-lg border bg-muted/20 p-3'
    >
      <Info className='mt-0.5 size-4 shrink-0 text-muted-foreground' />
      <div className='min-w-0 text-muted-foreground text-sm'>
        <p>{t('contentFaq.insight.hint')}</p>
        <p className='mt-1 truncate font-medium text-foreground/80'>
          「{question}」
        </p>
      </div>
    </div>
  )
}
