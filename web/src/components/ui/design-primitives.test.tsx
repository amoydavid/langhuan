import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { Button } from './button'
import { Card, CardContent } from './card'
import { Input } from './input'
import { Table, TableHead, TableHeader, TableRow } from './table'

describe('engineering console primitives', () => {
  it('renders flat compact action controls', async () => {
    const screen = await render(
      <div>
        <Button>创建知识库</Button>
        <Input aria-label='名称' />
      </div>
    )

    const button = screen.getByRole('button', { name: '创建知识库' }).element()
    const input = screen.getByRole('textbox', { name: '名称' }).element()

    expect(button.className).toContain('shadow-none')
    expect(button.className).toContain('active:translate-y-px')
    expect(input.className).toContain('h-[34px]')
    expect(input.className).toContain('shadow-none')
  })

  it('renders dense border-led cards without a default shadow', async () => {
    await render(
      <Card>
        <CardContent>内容</CardContent>
      </Card>
    )

    const card = document.querySelector('[data-slot="card"]')
    const content = document.querySelector('[data-slot="card-content"]')
    expect(card?.className).toContain('rounded-[10px]')
    expect(card?.className).toContain('py-4')
    expect(card?.className).toContain('shadow-none')
    expect(content?.className).toContain('px-4')
  })

  it('keeps table headers visible on dense data pages', async () => {
    await render(
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>文档</TableHead>
          </TableRow>
        </TableHeader>
      </Table>
    )

    const header = document.querySelector('[data-slot="table-header"]')
    expect(header?.className).toContain('sticky')
    expect(header?.className).toContain('bg-surface-2')
  })
})
