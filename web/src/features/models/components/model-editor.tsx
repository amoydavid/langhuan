import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import type { Model, ModelProvider, ModelScope, ModelType } from '../types'
import { ModelForm } from './model-form'

type ModelEditorProps = {
  provider: ModelProvider
  scope: ModelScope
  workspaceSlug?: string
  model?: Model
  onSaved?: (model: Model) => void
}

export function ModelEditor(props: ModelEditorProps) {
  const supported = props.provider.capabilities.filter(
    (capability): capability is ModelType =>
      capability === 'embedding' || capability === 'rerank'
  )
  const [selectedType, setSelectedType] = useState<ModelType>(
    props.model?.type ?? supported[0] ?? 'embedding'
  )

  if (supported.length === 0) {
    return (
      <p className='rounded-lg border border-dashed p-6 text-muted-foreground text-sm'>
        此连接不提供模型能力。
      </p>
    )
  }

  return (
    <div className='space-y-5'>
      {props.model ? (
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-sm'>模型类型</span>
          <Badge variant='secondary'>
            {props.model.type === 'embedding' ? 'Embedding' : 'Rerank'}
          </Badge>
          <span className='text-muted-foreground text-xs'>创建后不可更改</span>
        </div>
      ) : (
        supported.length > 1 && (
          <fieldset className='grid gap-2'>
            <legend className='mb-1 font-medium text-sm'>模型类型</legend>
            <div className='grid gap-3 sm:grid-cols-2'>
              {supported.map((type) => (
                <label
                  key={type}
                  className={`flex cursor-pointer items-start gap-3 rounded-lg border p-4 ${selectedType === type ? 'border-primary bg-primary/5' : ''}`}
                >
                  <input
                    type='radio'
                    name='model-type'
                    value={type}
                    checked={selectedType === type}
                    onChange={() => setSelectedType(type)}
                  />
                  <span>
                    <span className='block font-medium'>
                      {type === 'embedding' ? 'Embedding' : 'Rerank'}
                    </span>
                    <span className='mt-1 block text-muted-foreground text-xs'>
                      {type === 'embedding'
                        ? '把文本转换为用于检索的向量'
                        : '对召回候选进行相关性重排'}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
        )
      )}
      <ModelForm {...props} type={props.model?.type ?? selectedType} />
    </div>
  )
}
