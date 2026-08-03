import { afterEach, describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { ThemeProvider } from './theme-provider'

describe('ThemeProvider', () => {
  afterEach(() => {
    document.documentElement.classList.remove('light', 'dark')
    document.querySelector('[data-theme-provider-test]')?.remove()
    document.querySelector("meta[name='theme-color']")?.remove()
  })

  it('keeps the browser theme color in sync with the applied theme token', async () => {
    const themeStyles = document.createElement('style')
    themeStyles.dataset.themeProviderTest = 'true'
    themeStyles.textContent = `
      :root { --background: test-light; }
      .dark { --background: test-dark; }
    `
    document.head.append(themeStyles)

    const metaThemeColor = document.createElement('meta')
    metaThemeColor.name = 'theme-color'
    metaThemeColor.content = 'test-light'
    document.head.append(metaThemeColor)

    await render(
      <ThemeProvider defaultTheme='dark' storageKey='theme-provider-test'>
        <div>Theme content</div>
      </ThemeProvider>
    )

    await expect.poll(() => metaThemeColor.content).toBe('test-dark')
  })
})
