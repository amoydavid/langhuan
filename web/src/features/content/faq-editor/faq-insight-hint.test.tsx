import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { FAQInsightHint } from './faq-insight-hint'

describe('FAQInsightHint（健康面板零召回来源提示条，v1.4.0 §9.3.6）', () => {
  it('renders the insight hint when a question prefill is present', async () => {
    const screen = await render(<FAQInsightHint question='发票抬头怎么改' />)
    await expect
      .element(
        screen.getByText(
          '本页来自健康面板的零召回查询，保存后回到健康页查看空召回率变化。'
        )
      )
      .toBeVisible()
  })

  it('renders nothing for a plain create visit without the question param', async () => {
    const screen = await render(<FAQInsightHint />)
    await expect
      .element(screen.getByTestId('faq-insight-hint'))
      .not.toBeInTheDocument()
  })
})
