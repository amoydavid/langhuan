import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { DocumentStatusBadge } from './document-list'

describe('DocumentStatusBadge', () => {
  it.each([
    ['ready', 'success'],
    ['pending', 'warning'],
    ['processing', 'info'],
    ['failed', 'danger'],
    ['deleted', 'neutral'],
  ] as const)('maps %s to the %s semantic tone', async (status, tone) => {
    await render(<DocumentStatusBadge status={status} />)

    const badge = document.querySelector('[data-slot="status-badge"]')
    expect(badge?.getAttribute('data-tone')).toBe(tone)
    expect(
      badge?.querySelector('[data-slot="status-indicator"]')
    ).not.toBeNull()
  })
})
