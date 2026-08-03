import { beforeEach, describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import i18n from '@/lib/i18n'
import { LanguageSwitch } from './language-switch'

describe('LanguageSwitch', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('zh')
  })

  it('打开菜单后展示两种语言选项并勾选当前语言', async () => {
    const { getByRole } = await render(<LanguageSwitch />)
    await userEvent.click(getByRole('button', { name: '切换语言' }))
    await expect
      .element(getByRole('menuitem', { name: '简体中文' }))
      .toBeVisible()
    await expect
      .element(getByRole('menuitem', { name: 'English' }))
      .toBeVisible()
  })

  it('点击 English 后切换为英文', async () => {
    const { getByRole } = await render(<LanguageSwitch />)
    await userEvent.click(getByRole('button', { name: '切换语言' }))
    await userEvent.click(getByRole('menuitem', { name: 'English' }))
    expect(i18n.language).toBe('en')
  })
})
