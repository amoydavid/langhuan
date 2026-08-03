import { expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { useMediaQuery } from './use-media-query'

function Probe() {
  const mobile = useMediaQuery('(max-width: 767px)')
  return <div>{mobile ? 'mobile' : 'not-mobile'}</div>
}

it('tracks a browser media query without requiring component-local resize state', async () => {
  const screen = await render(<Probe />)
  await expect.element(screen.getByText('mobile')).toBeVisible()
})
