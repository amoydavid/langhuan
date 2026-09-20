import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import type { WebSourceHealth } from '../config'

export function healthBadgeTone(
  health: WebSourceHealth
): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (health) {
    case 'ok':
      return 'success'
    case 'partial':
      return 'warning'
    case 'failed':
      return 'danger'
    case 'disabled':
      return 'neutral'
  }
}

/** 源健康度徽标：正常/部分失败/失败/已停用（spec §9.5.1）。 */
export function HealthBadge({ health }: { health: WebSourceHealth }) {
  const { t } = useTranslation()
  return (
    <StatusBadge tone={healthBadgeTone(health)}>
      {t(`webSource.health.${health}`)}
    </StatusBadge>
  )
}
