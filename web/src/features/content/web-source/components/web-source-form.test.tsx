import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, test, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { WebSourceForm } from './web-source-form'

vi.mock('@/features/integrations/queries', () => ({
  sourceConnectionsQueryOptions: () => ({
    queryKey: ['source-connections', 'probe'],
    queryFn: async () => [],
  }),
}))

function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <WebSourceForm workspaceSlug='acme' kbId='kb-1' onDone={() => {}} />
    </QueryClientProvider>
  )
}

// 回归（2026-09-18 用户实测）：卡片式 radio 的点击区域必须是整卡（含文字
// 与描述），而不是只有 radio 圆点——此前 FormControl slot 把字段 id 注入
// 卡片 label（for 指向 radiogroup 容器），点文字无效且 radio 无可访问名称。
test('点击导入模式卡片的文字区域即可选中该模式', async () => {
  const screen = await renderForm()
  const radio = screen.getByRole('radio', { name: /Sitemap/ })
  await expect.element(radio).toBeInTheDocument()

  // 点卡片上的文字（非圆点本身）。
  await screen.getByText('Sitemap', { exact: true }).click()
  await expect.element(radio).toBeChecked()
})

test('点击卡片描述文字同样可选中', async () => {
  const screen = await renderForm()
  // 「站点地图自动发现」是 sitemap 卡片的描述文字。
  await screen.getByText('站点地图自动发现').click()
  const radio = screen.getByRole('radio', { name: /Sitemap/ })
  await expect.element(radio).toBeChecked()
})
