import { describe, expect, it } from 'vitest'
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
})
