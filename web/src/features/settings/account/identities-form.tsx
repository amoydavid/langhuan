import { useQuery } from '@tanstack/react-query'
import { KeyRound, Link2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { startOIDCBind } from '@/features/auth/api'
import { externalIdentitiesQueryOptions } from '@/features/auth/queries'

export function IdentitiesForm({ oidcEnabled }: { oidcEnabled: boolean }) {
  const { t } = useTranslation()
  const { data: identities, isLoading } = useQuery(
    externalIdentitiesQueryOptions()
  )

  if (!oidcEnabled) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('settings.account.ssoDisabled')}
      </p>
    )
  }

  return (
    <div className='space-y-4'>
      {isLoading ? (
        <p className='text-muted-foreground text-sm'>
          {t('settings.account.loadingIdentities')}
        </p>
      ) : identities && identities.length > 0 ? (
        <ul className='space-y-2'>
          {identities.map((id) => (
            <li
              key={`${id.issuer}-${id.email}`}
              className='flex items-center justify-between rounded-md border border-border p-3'
            >
              <div className='flex items-center gap-2'>
                <KeyRound className='size-4 text-muted-foreground' />
                <div>
                  <p className='font-medium text-sm'>{id.email}</p>
                  <p className='text-muted-foreground text-xs'>{id.issuer}</p>
                </div>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t('settings.account.noIdentities')}
        </p>
      )}
      <Button
        type='button'
        variant='outline'
        onClick={() => {
          toast.info(t('settings.account.bindRedirecting'))
          startOIDCBind()
        }}
      >
        <Link2 />
        {t('settings.account.bindSSO')}
      </Button>
    </div>
  )
}
