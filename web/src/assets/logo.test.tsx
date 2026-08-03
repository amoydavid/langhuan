import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { Logo } from './logo'

describe('Logo', () => {
  it('uses the Langhuan product name as its accessible title', async () => {
    await render(<Logo />)

    expect(document.querySelector('#langhuan-logo title')?.textContent).toBe(
      '琅嬛'
    )
  })

  it('renders the approved fill artwork by default', async () => {
    await render(<Logo />)

    const logo = document.querySelector('[data-logo-variant="fill"]')
    expect(logo?.querySelector('path')?.getAttribute('fill')).toBe('#00863b')
    expect(logo?.querySelectorAll('path')[1]?.getAttribute('fill')).toBe(
      '#ffffff'
    )
  })

  it('renders the line artwork with the current context color', async () => {
    await render(<Logo variant='line' />)

    const logo = document.querySelector('[data-logo-variant="line"]')
    expect(logo?.querySelector('path')?.getAttribute('stroke')).toBe(
      'currentColor'
    )
    expect(logo?.querySelector('path')?.getAttribute('stroke-width')).toBe('6')
  })
})
