import type { TFunction } from 'i18next'
import type { ProviderFormValues } from '../schemas'
import { parseCustomHeadersText } from '../schemas/openai'
import type {
  CreateModelProviderInput,
  ModelProvider,
  ModelScope,
  ProviderKey,
  UpdateModelProviderInput,
} from '../types'

export function providerLabels(t: TFunction): Record<ProviderKey, string> {
  return {
    openai: t('models.common.providerOpenAIOrAzure'),
    ark: t('models.common.providerArk'),
    ollama: t('models.common.providerOllama'),
    dashscope: t('models.common.providerDashscope'),
    tencentcloud: t('models.common.providerTencentcloud'),
    rerank_compatible: t('models.common.providerRerankCompatible'),
    siliconflow: 'SiliconFlow',
    mineru: t('models.common.providerMinerU'),
  }
}

function configString(provider: ModelProvider | undefined, key: string) {
  const value = provider?.config[key]
  return typeof value === 'string' ? value : ''
}

function configNumber(
  provider: ModelProvider | undefined,
  key: string,
  fallback: number
) {
  const value = provider?.config[key]
  return typeof value === 'number' ? value : fallback
}

export function providerFormDefaults(
  scope: ModelScope,
  providerKey: ProviderKey,
  provider?: ModelProvider
): ProviderFormValues {
  const common = {
    scope,
    name: provider?.name ?? '',
    display_name: provider?.display_name ?? '',
    description: provider?.description ?? '',
    replace_credentials: provider === undefined,
  }
  switch (providerKey) {
    case 'openai':
      return {
        ...common,
        provider: 'openai',
        mode: configString(provider, 'mode') === 'azure' ? 'azure' : 'standard',
        base_url: configString(provider, 'base_url'),
        api_version: configString(provider, 'api_version'),
        timeout_seconds: configNumber(provider, 'timeout_seconds', 60),
        api_key: '',
        custom_headers: '',
      }
    case 'ark':
      return {
        ...common,
        provider: 'ark',
        base_url: configString(provider, 'base_url'),
        region: configString(provider, 'region') || 'cn-beijing',
        auth_mode:
          configString(provider, 'auth_mode') === 'ak_sk' ? 'ak_sk' : 'api_key',
        timeout_seconds: configNumber(provider, 'timeout_seconds', 60),
        retry_times: configNumber(provider, 'retry_times', 2),
        api_key: '',
        access_key: '',
        secret_key: '',
      }
    case 'ollama':
      return {
        ...common,
        scope: 'platform',
        provider: 'ollama',
        base_url:
          configString(provider, 'base_url') || 'http://localhost:11434',
        timeout_seconds: configNumber(provider, 'timeout_seconds', 60),
      }
    case 'dashscope':
      return {
        ...common,
        provider: 'dashscope',
        timeout_seconds: configNumber(provider, 'timeout_seconds', 60),
        api_key: '',
      }
    case 'tencentcloud':
      return {
        ...common,
        provider: 'tencentcloud',
        region: configString(provider, 'region') || 'ap-guangzhou',
        secret_id: '',
        secret_key: '',
      }
    case 'mineru':
      return {
        ...common,
        provider: 'mineru',
        base_url: configString(provider, 'base_url') || 'https://mineru.net',
        model_version: (configString(provider, 'model_version') === 'pipeline'
          ? 'pipeline'
          : 'vlm') as 'vlm' | 'pipeline',
        token: '',
      }
    case 'siliconflow':
      return {
        ...common,
        provider: 'siliconflow',
        base_url:
          configString(provider, 'base_url') || 'https://api.siliconflow.cn',
        embedding_endpoint_path:
          configString(provider, 'embedding_endpoint_path') || '/v1/embeddings',
        rerank_endpoint_path:
          configString(provider, 'rerank_endpoint_path') || '/v1/rerank',
        timeout_seconds: configNumber(provider, 'timeout_seconds', 60),
        retry_times: configNumber(provider, 'retry_times', 2),
        api_key: '',
      }
    case 'rerank_compatible':
      return {
        ...common,
        provider: 'rerank_compatible',
        base_url: configString(provider, 'base_url'),
        endpoint_path: configString(provider, 'endpoint_path') || '/v1/rerank',
        timeout_seconds: configNumber(provider, 'timeout_seconds', 30),
        retry_times: configNumber(provider, 'retry_times', 2),
        api_key: '',
        custom_headers: '',
      }
  }
}

export function toCreateProviderRequest(
  values: ProviderFormValues
): CreateModelProviderInput {
  const common = {
    name: values.name,
    display_name: values.display_name,
    description: values.description,
  }
  switch (values.provider) {
    case 'openai': {
      const customHeaders = parseCustomHeadersText(values.custom_headers)
      return {
        ...common,
        provider: 'openai',
        config: {
          mode: values.mode,
          base_url: values.base_url || undefined,
          api_version: values.api_version || undefined,
          timeout_seconds: values.timeout_seconds,
        },
        credentials: {
          api_key: values.api_key,
          ...(Object.keys(customHeaders).length > 0
            ? { custom_headers: customHeaders }
            : {}),
        },
      }
    }
    case 'ark':
      return {
        ...common,
        provider: 'ark',
        config: {
          base_url: values.base_url || undefined,
          region: values.region,
          auth_mode: values.auth_mode,
          timeout_seconds: values.timeout_seconds,
          retry_times: values.retry_times,
        },
        credentials:
          values.auth_mode === 'api_key'
            ? { api_key: values.api_key ?? '' }
            : {
                access_key: values.access_key ?? '',
                secret_key: values.secret_key ?? '',
              },
      }
    case 'ollama':
      return {
        ...common,
        provider: 'ollama',
        config: {
          base_url: values.base_url,
          timeout_seconds: values.timeout_seconds,
        },
        credentials: {},
      }
    case 'dashscope':
      return {
        ...common,
        provider: 'dashscope',
        config: { timeout_seconds: values.timeout_seconds },
        credentials: { api_key: values.api_key },
      }
    case 'tencentcloud':
      return {
        ...common,
        provider: 'tencentcloud',
        config: { region: values.region },
        credentials: {
          secret_id: values.secret_id,
          secret_key: values.secret_key,
        },
      }
    case 'mineru':
      return {
        ...common,
        provider: 'mineru',
        config: {
          base_url: values.base_url || undefined,
          model_version: values.model_version,
        },
        credentials: { token: values.token },
      }
    case 'siliconflow':
      return {
        ...common,
        provider: 'siliconflow',
        config: {
          base_url: values.base_url,
          embedding_endpoint_path: values.embedding_endpoint_path,
          rerank_endpoint_path: values.rerank_endpoint_path,
          timeout_seconds: values.timeout_seconds,
          retry_times: values.retry_times,
        },
        credentials: { api_key: values.api_key },
      }
    case 'rerank_compatible': {
      const customHeaders = parseCustomHeadersText(values.custom_headers)
      return {
        ...common,
        provider: 'rerank_compatible',
        config: {
          base_url: values.base_url || undefined,
          endpoint_path: values.endpoint_path,
          timeout_seconds: values.timeout_seconds,
          retry_times: values.retry_times,
        },
        credentials: {
          api_key: values.api_key,
          ...(Object.keys(customHeaders).length > 0
            ? { custom_headers: customHeaders }
            : {}),
        },
      }
    }
  }
}

export function toUpdateProviderRequest(
  values: ProviderFormValues
): UpdateModelProviderInput {
  const created = toCreateProviderRequest(values)
  return {
    display_name: created.display_name,
    description: created.description,
    config: created.config,
    ...(values.replace_credentials ? { credentials: created.credentials } : {}),
  }
}
