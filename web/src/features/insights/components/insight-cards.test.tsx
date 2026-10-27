import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { InsightsOverview } from '../types'
import {
  emptyRecallDelta,
  emptyRecallTrend,
  InsightCards,
} from './insight-cards'

function makeOverview(
  overrides: Partial<InsightsOverview> = {}
): InsightsOverview {
  return {
    window_days: 14,
    runs_total: 128,
    runs_available: 100,
    runs_empty: 20,
    runs_degraded: 8,
    runs_failed_excluded: 5,
    empty_recall_rate: 0.156,
    previous_empty_recall_rate: 0.2,
    has_previous_window: true,
    sleeping_documents_count: 3,
    sleeping_insufficient_sample: false,
    recent_changes_count: 12,
    query_log_enabled: false,
    ...overrides,
  }
}

describe('InsightCards 环比纯函数', () => {
  it('下降窗口给出 down 趋势与负差值（百分点）', () => {
    const overview = makeOverview()
    expect(emptyRecallTrend(overview)).toBe('down')
    expect(emptyRecallDelta(overview)).toBeCloseTo(-4.4, 10)
  })

  it('上升窗口给出 up 趋势', () => {
    const overview = makeOverview({
      empty_recall_rate: 0.2,
      previous_empty_recall_rate: 0.156,
    })
    expect(emptyRecallTrend(overview)).toBe('up')
  })

  it('无上期窗口或持平时不出趋势', () => {
    expect(
      emptyRecallTrend(makeOverview({ has_previous_window: false }))
    ).toBeNull()
    expect(
      emptyRecallTrend(
        makeOverview({
          empty_recall_rate: 0.2,
          previous_empty_recall_rate: 0.2,
        })
      )
    ).toBeNull()
  })
})

describe('InsightCards', () => {
  it('渲染四张计数卡：检索量、空召回率、沉睡内容、变更数', async () => {
    const screen = await render(
      <InsightCards overview={makeOverview()} windowDays={14} />
    )

    await expect.element(screen.getByText('近 14 天检索')).toBeVisible()
    await expect.element(screen.getByText('128', { exact: true })).toBeVisible()
    await expect.element(screen.getByText('空召回 20 · 降级 8')).toBeVisible()
    await expect
      .element(screen.getByText('15.6%', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('沉睡内容')).toBeVisible()
    await expect.element(screen.getByText('3', { exact: true })).toBeVisible()
    await expect.element(screen.getByText('近 14 天变更')).toBeVisible()
    await expect.element(screen.getByText('12', { exact: true })).toBeVisible()
  })

  it('空召回率下降时展示绿色向下箭头与较上期文案', async () => {
    await render(<InsightCards overview={makeOverview()} windowDays={14} />)

    const trend = document.querySelector('[data-testid="empty-recall-trend"]')
    expect(trend?.getAttribute('data-direction')).toBe('down')
    expect(trend?.getAttribute('class')).toContain('text-success')
    const delta = document.querySelector('[data-testid="empty-recall-delta"]')
    expect(delta?.textContent).toBe('较上期 -4.4pp')
    expect(delta?.getAttribute('class')).toContain('text-success')
  })

  it('空召回率上升时展示红色向上箭头', async () => {
    await render(
      <InsightCards
        overview={makeOverview({
          empty_recall_rate: 0.2,
          previous_empty_recall_rate: 0.156,
        })}
        windowDays={14}
      />
    )

    const trend = document.querySelector('[data-testid="empty-recall-trend"]')
    expect(trend?.getAttribute('data-direction')).toBe('up')
    expect(trend?.getAttribute('class')).toContain('text-destructive')
    const delta = document.querySelector('[data-testid="empty-recall-delta"]')
    expect(delta?.textContent).toBe('较上期 +4.4pp')
    expect(delta?.getAttribute('class')).toContain('text-destructive')
  })

  it('无上期窗口时不出箭头，显示占位说明', async () => {
    const screen = await render(
      <InsightCards
        overview={makeOverview({
          empty_recall_rate: 0.2,
          has_previous_window: false,
        })}
        windowDays={14}
      />
    )

    expect(
      document.querySelector('[data-testid="empty-recall-trend"]')
    ).toBeNull()
    // has_previous_window=false：不出环比，显示暂无上期对比。
    await expect.element(screen.getByText('暂无上期对比')).toBeVisible()
  })

  it('持平且样本不足时沉睡卡切换为样本不足说明', async () => {
    const screen = await render(
      <InsightCards
        overview={makeOverview({
          empty_recall_rate: 0.2,
          previous_empty_recall_rate: 0.2,
          sleeping_insufficient_sample: true,
        })}
        windowDays={7}
      />
    )

    // 持平：不出现环比箭头，回退显示上期值。
    expect(
      document.querySelector('[data-testid="empty-recall-trend"]')
    ).toBeNull()
    await expect.element(screen.getByText('上期 20.0%')).toBeVisible()
    await expect
      .element(screen.getByText('检索样本不足，暂无法判定'))
      .toBeVisible()
    await expect.element(screen.getByText('近 7 天检索')).toBeVisible()
  })
})
