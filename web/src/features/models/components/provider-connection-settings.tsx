import { KeyRound, RadioTower } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { ModelProvider } from '../types'

const readableConfig: Record<string, string> = {
  base_url: 'API 地址',
  mode: '连接模式',
  region: '区域',
  api_version: 'API 版本',
  embedding_endpoint_path: 'Embedding 路径',
  rerank_endpoint_path: 'Rerank 路径',
  endpoint_path: '接口路径',
  timeout_seconds: '超时（秒）',
  retry_times: '重试次数',
  model_version: '模型版本',
}

export function ProviderConnectionSettings({
  provider,
}: {
  provider: ModelProvider
}) {
  const entries = Object.entries(readableConfig).flatMap(([key, label]) => {
    const value = provider.config[key]
    return typeof value === 'string' || typeof value === 'number'
      ? [{ key, label, value }]
      : []
  })
  return (
    <div className='grid gap-4 lg:grid-cols-[1.2fr_0.8fr]'>
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <RadioTower className='size-4 text-primary' /> 连接配置
          </CardTitle>
        </CardHeader>
        <CardContent>
          {entries.length === 0 ? (
            <p className='text-muted-foreground text-sm'>没有额外连接参数</p>
          ) : (
            <dl className='grid gap-4 text-sm sm:grid-cols-2'>
              {entries.map((entry) => (
                <div key={entry.key}>
                  <dt className='text-muted-foreground text-xs'>
                    {entry.label}
                  </dt>
                  <dd className='mt-1 break-all'>{entry.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <KeyRound className='size-4 text-primary' /> 凭证
          </CardTitle>
        </CardHeader>
        <CardContent className='space-y-2 text-sm'>
          <p className='font-medium'>
            {provider.credential_fields.length === 0
              ? '此连接无需凭证'
              : provider.credentials_configured
                ? '凭证已加密保存'
                : '凭证尚未配置'}
          </p>
          <p className='text-muted-foreground text-xs'>
            已保存的凭证不会回显。轮换时提交的新凭证会整体替换旧凭证。
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
