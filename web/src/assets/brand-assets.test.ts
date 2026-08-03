import { describe, expect, it } from 'vitest'
import indexHtml from '../../index.html?raw'

async function loadAsset(path: string) {
  const response = await fetch(path)
  expect(response.ok).toBe(true)
  return response.text()
}

describe('Langhuan brand assets', () => {
  it('serves the circular favicon artwork', async () => {
    const favicon = await loadAsset('/images/favicon.svg')

    expect(favicon).toContain('<circle cx="50" cy="50" r="50" fill="#00863b"')
    expect(favicon).toContain(
      'M32 22h36a10 10 0 0 1 10 10v20a10 10 0 0 1-10 10H50L36 78V62h-4a10 10 0 0 1-10-10V32a10 10 0 0 1 10-10z'
    )
  })

  it('serves the fill and line logo variants', async () => {
    const [fillLogo, lineLogo] = await Promise.all([
      loadAsset('/images/logo-fill.svg'),
      loadAsset('/images/logo-line.svg'),
    ])

    expect(fillLogo).toContain('<path fill="#00863b"')
    expect(fillLogo).toContain(
      '<path fill="#ffffff" d="M54 22h14v22l-7-6-7 6V22z"'
    )
    expect(lineLogo).toContain('stroke="#00863b"')
    expect(lineLogo).toContain('stroke-width="6"')
    expect(lineLogo).toContain('d="M54 22v22l7-6 7 6v-22"')
  })

  it('uses one theme-independent SVG favicon reference', () => {
    expect(indexHtml.match(/rel="icon"/g)).toHaveLength(1)
    expect(indexHtml).toContain('href="/images/favicon.svg"')
    expect(indexHtml).not.toContain('favicon_light')
    expect(indexHtml).not.toContain('favicon.png')
  })
})
