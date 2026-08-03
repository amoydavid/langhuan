import { expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { SafeMarkdown } from './safe-markdown'

it('sanitizes dangerous HTML and links while preserving GFM content', async () => {
  await render(
    <section data-testid='rendered-markdown'>
      <SafeMarkdown
        content={[
          '<script>alert(1)</script>',
          '[bad](javascript:alert(1))',
          '',
          '~~旧内容~~',
          '',
          '| 字段 | 值 |',
          '| --- | --- |',
          '| 状态 | 可检索 |',
        ].join('\n')}
      />
    </section>
  )

  const output = document.querySelector('[data-testid="rendered-markdown"]')
  expect(output?.querySelector('script')).toBeNull()
  expect(output?.querySelector('a')).toBeNull()
  expect(output?.querySelector('del')?.textContent).toBe('旧内容')
  expect(output?.querySelector('table')?.textContent).toContain('可检索')
})
