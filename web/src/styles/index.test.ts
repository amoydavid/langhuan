import { describe, expect, it } from 'vitest'
import styles from './index.css?raw'

describe('product presentation utilities', () => {
  it.each(['page-eyebrow', 'resource-card', 'icon-tile', 'code-panel'])(
    'defines the %s utility',
    (name) => {
      expect(styles).toContain(`@utility ${name}`)
    }
  )

  it('uses the compact console stroke weight for Lucide icons', () => {
    expect(styles).toContain('svg.lucide')
    expect(styles).toContain('stroke-width: 1.5')
  })
})
