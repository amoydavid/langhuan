import { useQuery } from '@tanstack/react-query'
import { Globe2, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { webSourcesQueryOptions } from '../queries'
import { WebSourceCard } from './web-source-card'
import { WebSourceFormDialog } from './web-source-form-dialog'
import { WebSourceTable } from './web-source-table'

type WebSourceListProps = {
  workspaceSlug: string
  kbId: string
  canManage: boolean
}

/** Web 子 tab = 抓取请求列表（spec §9.5.1）：源卡片列表 + 添加入口 + 空态。 */
export function WebSourceList({
  workspaceSlug,
  kbId,
  canManage,
}: WebSourceListProps) {
  const { t } = useTranslation()
  const { data: views = [], isPending } = useQuery(
    webSourcesQueryOptions(workspaceSlug, kbId)
  )
  const [addOpen, setAddOpen] = useState(false)

  if (isPending) {
    return (
      <div className='grid gap-4'>
        {Array.from({ length: 2 }).map((_, index) => (
          <Card key={`web-source-skeleton-${index}`}>
            <CardContent className='space-y-2 p-4'>
              <Skeleton className='h-5 w-1/3' />
              <Skeleton className='h-4 w-2/3' />
              <Skeleton className='h-4 w-1/2' />
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  if (views.length === 0) {
    return (
      <>
        <Card className='border-dashed'>
          <CardContent className='flex min-h-48 flex-col items-center justify-center text-center'>
            <div className='mb-4 flex size-11 items-center justify-center rounded-lg bg-muted'>
              <Globe2 className='size-5 text-muted-foreground' />
            </div>
            <h2 className='font-medium'>{t('webSource.list.emptyTitle')}</h2>
            <p className='mt-2 max-w-md text-muted-foreground text-sm'>
              {t('webSource.list.emptyDescription')}
            </p>
            {canManage && (
              <Button className='mt-5' onClick={() => setAddOpen(true)}>
                <Plus />
                {t('webSource.list.emptyAddButton')}
              </Button>
            )}
          </CardContent>
        </Card>
        <WebSourceFormDialog
          workspaceSlug={workspaceSlug}
          kbId={kbId}
          open={addOpen}
          onOpenChange={setAddOpen}
        />
      </>
    )
  }

  return (
    <div className='space-y-4'>
      {canManage && (
        <div className='flex justify-end'>
          <Button type='button' size='sm' onClick={() => setAddOpen(true)}>
            <Plus />
            {t('webSource.list.addButton')}
          </Button>
        </div>
      )}
      {/* PC 表格式（与其它内容 tab 一致）/ 移动端卡片流 */}
      <div className='hidden md:block'>
        <WebSourceTable
          views={views}
          workspaceSlug={workspaceSlug}
          kbId={kbId}
          canManage={canManage}
        />
      </div>
      <div className='grid gap-4 md:hidden'>
        {views.map((view) => (
          <WebSourceCard
            key={view.source.id}
            view={view}
            workspaceSlug={workspaceSlug}
            kbId={kbId}
            canManage={canManage}
          />
        ))}
      </div>
      <WebSourceFormDialog
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        open={addOpen}
        onOpenChange={setAddOpen}
      />
    </div>
  )
}
