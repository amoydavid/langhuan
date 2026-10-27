import { TrendingDown, TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { InsightsOverview } from '../types'

type InsightCardsProps = {
  overview: InsightsOverview
  windowDays: number
}

/** 空召回率展示为百分比，保留一位小数。 */
export function formatEmptyRecallRate(rate: number) {
  return `${(rate * 100).toFixed(1)}%`
}

/**
 * 与上一窗口的空召回率差值（百分点）。无上期窗口或上期值缺失时返回 null。
 */
export function emptyRecallDelta(overview: InsightsOverview): number | null {
  if (
    !overview.has_previous_window ||
    overview.previous_empty_recall_rate == null
  ) {
    return null
  }
  return (
    (overview.empty_recall_rate - overview.previous_empty_recall_rate) * 100
  )
}

/** 环比方向：下降（改善）= down，上升（恶化）= up；无上期或持平为 null。 */
export function emptyRecallTrend(
  overview: InsightsOverview
): 'down' | 'up' | null {
  const delta = emptyRecallDelta(overview)
  if (delta == null) return null
  // 展示保留一位小数，半分以内视为持平，不出箭头。
  if (Math.abs(delta) < 0.05) return null
  return delta < 0 ? 'down' : 'up'
}

/**
 * 健康页四张计数卡（spec §9.3.1）：近 N 天检索、空召回率（含环比）、
 * 沉睡内容、近 N 天变更。骨架复用 Overview 计数卡（标签在上、数字在下）。
 */
export function InsightCards({ overview, windowDays }: InsightCardsProps) {
  const { t } = useTranslation()
  const trend = emptyRecallTrend(overview)
  const delta = emptyRecallDelta(overview)
  const deltaText =
    trend && delta != null
      ? t(
          trend === 'down'
            ? 'insights.cards.deltaDown'
            : 'insights.cards.deltaUp',
          {
            delta: Math.abs(delta).toFixed(1),
          }
        )
      : null
  const previousRate =
    overview.has_previous_window && overview.previous_empty_recall_rate != null
      ? formatEmptyRecallRate(overview.previous_empty_recall_rate)
      : null

  return (
    <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
      <Card className='h-full'>
        <CardHeader className='gap-1 pb-3'>
          <CardDescription>
            {t('insights.cards.runsTitle', { window: windowDays })}
          </CardDescription>
          <CardTitle className='text-2xl'>
            {overview.runs_total.toLocaleString()}
          </CardTitle>
          <p className='text-muted-foreground text-xs'>
            {t('insights.cards.runsSub', {
              empty: overview.runs_empty,
              degraded: overview.runs_degraded,
            })}
          </p>
        </CardHeader>
      </Card>

      <Card className='h-full'>
        <CardHeader className='gap-1 pb-3'>
          <CardDescription>
            {t('insights.cards.emptyRateTitle')}
          </CardDescription>
          <CardTitle className='flex items-center gap-1.5 text-2xl'>
            {formatEmptyRecallRate(overview.empty_recall_rate)}
            {trend === 'down' && (
              <TrendingDown
                data-testid='empty-recall-trend'
                data-direction='down'
                aria-hidden
                className='size-5 text-success'
              />
            )}
            {trend === 'up' && (
              <TrendingUp
                data-testid='empty-recall-trend'
                data-direction='up'
                aria-hidden
                className='size-5 text-destructive'
              />
            )}
          </CardTitle>
          {deltaText ? (
            <p
              data-testid='empty-recall-delta'
              className={cn(
                'text-xs',
                trend === 'down' ? 'text-success' : 'text-destructive'
              )}
            >
              {deltaText}
            </p>
          ) : (
            <p className='text-muted-foreground text-xs'>
              {previousRate
                ? t('insights.cards.emptyRatePrevious', { rate: previousRate })
                : t('insights.cards.emptyRateNoPrevious')}
            </p>
          )}
        </CardHeader>
      </Card>

      <Card className='h-full'>
        <CardHeader className='gap-1 pb-3'>
          <CardDescription>{t('insights.cards.sleepingTitle')}</CardDescription>
          <CardTitle className='text-2xl'>
            {overview.sleeping_documents_count.toLocaleString()}
          </CardTitle>
          <p className='text-muted-foreground text-xs'>
            {overview.sleeping_insufficient_sample
              ? t('insights.cards.sleepingInsufficient')
              : t('insights.cards.sleepingSub')}
          </p>
        </CardHeader>
      </Card>

      <Card className='h-full'>
        <CardHeader className='gap-1 pb-3'>
          <CardDescription>
            {t('insights.cards.changesTitle', { window: windowDays })}
          </CardDescription>
          <CardTitle className='text-2xl'>
            {overview.recent_changes_count.toLocaleString()}
          </CardTitle>
          <p className='text-muted-foreground text-xs'>
            {t('insights.cards.changesSub')}
          </p>
        </CardHeader>
      </Card>
    </div>
  )
}
