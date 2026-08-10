import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ThemeProvider } from '@/context/theme-provider'
import { AppearanceForm } from './appearance-form'

describe('AppearanceForm', () => {
  it('keeps only the three supported theme choices', async () => {
    const screen = await render(
      <ThemeProvider>
        <AppearanceForm />
      </ThemeProvider>
    )

    expect(document.querySelector('select')).toBeNull()
    await expect
      .element(screen.getByRole('radio', { name: '浅色' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('radio', { name: '深色' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('radio', { name: '跟随系统' }))
      .toBeInTheDocument()
  })

  it('switches the theme when a card is clicked', async () => {
    const screen = await render(
      <ThemeProvider>
        <AppearanceForm />
      </ThemeProvider>
    )

    // 默认主题为 system，浅色卡片初始未选中
    const lightRadio = screen.getByRole('radio', { name: '浅色' })
    await expect.element(lightRadio).not.toBeChecked()

    // 点击卡片文字区域应切换主题（回归：此前 FormLabel htmlFor 指向 radiogroup 根，点击无效）
    await userEvent.click(screen.getByText('浅色'))

    await expect.element(lightRadio).toBeChecked()
  })
})
