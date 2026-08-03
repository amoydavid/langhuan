import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { apiKeyStatusBadgeVariant, apiKeyStatusLabel } from '../display'
import type { APIKeyStatus } from '../types'

export function APIKeyStatusBadge({ status }: { status: APIKeyStatus }) {
  const { t } = useTranslation()
  return (
    <Badge variant={apiKeyStatusBadgeVariant[status]}>
      {apiKeyStatusLabel(t)[status]}
    </Badge>
  )
}
