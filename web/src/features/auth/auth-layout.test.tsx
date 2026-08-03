import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { AuthLayout } from './auth-layout'

describe('AuthLayout', () => {
  it('stretches the auth content track while centering its children', async () => {
    const screen = await render(
      <AuthLayout>
        <div>认证内容</div>
      </AuthLayout>
    )
    const content = screen.getByText('认证内容').element()
    const contentWrapper = content.parentElement

    if (contentWrapper === null || contentWrapper.parentElement === null) {
      throw new Error('认证布局缺少内容容器')
    }

    const layout = contentWrapper.parentElement

    await expect.element(layout).not.toHaveClass('justify-center')
    await expect.element(layout).toHaveClass('border-primary')
    await expect.element(contentWrapper).toHaveClass('items-center')
    expect(document.querySelector('[data-slot="auth-mark"]')).not.toBeNull()
    expect(
      document.querySelector(
        '[data-slot="auth-mark"] [data-logo-variant="fill"]'
      )
    ).not.toBeNull()
  })
})
