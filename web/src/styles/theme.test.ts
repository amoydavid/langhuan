import { describe, expect, it } from 'vitest'
import themeCss from './theme.css?raw'

describe('engineering green theme contract', () => {
  it('defines the approved light and dark engineering-green foundations', () => {
    expect(themeCss).toContain('--primary: oklch(0.54 0.15 150)')
    expect(themeCss).toContain('--sidebar: oklch(0.22 0.012 160)')
    expect(themeCss).toContain('--background: oklch(0.16 0.012 160)')
    expect(themeCss).toContain('--surface-2:')
    expect(themeCss).toContain('--border-strong:')
  })

  it('exposes semantic status colors to Tailwind', () => {
    for (const token of ['success', 'warning', 'danger', 'info']) {
      expect(themeCss).toContain(`--${token}:`)
      expect(themeCss).toContain(`--color-${token}: var(--${token})`)
    }
  })

  it('provides the brighter brand stroke used on the dark sidebar', () => {
    expect(themeCss).toContain('--sidebar-logo: oklch(0.665 0.16 152)')
    expect(themeCss).toContain('--color-sidebar-logo: var(--sidebar-logo)')
  })
})
