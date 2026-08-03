import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import i18n from '@/lib/i18n'
import { parseApiError } from './api/error'

type UnauthorizedHandler = () => boolean | undefined

let unauthorizedHandler: UnauthorizedHandler | undefined
let unauthorizedNavigationStarted = false

export function setUnauthorizedHandler(
  handler: UnauthorizedHandler | undefined
) {
  unauthorizedHandler = handler
}

export function resetUnauthorizedNavigation() {
  unauthorizedNavigationStarted = false
}

function coordinateUnauthorized(client: QueryClient) {
  if (unauthorizedNavigationStarted) return
  if (unauthorizedHandler?.() === false) return
  unauthorizedNavigationStarted = true
  client.clear()
  toast.error(i18n.t('common.sessionExpired'))
}

export function handleUnauthorizedOnce() {
  coordinateUnauthorized(queryClient)
}

function handleGlobalError(error: unknown, client: QueryClient) {
  const apiError = parseApiError(error)
  if (apiError.status === 401) {
    coordinateUnauthorized(client)
    return
  }
  if (apiError.status === 403) {
    toast.error(i18n.t('common.forbidden'))
    void client.invalidateQueries({ queryKey: ['me'] })
  }
}

export function createAppQueryClient() {
  let client: QueryClient
  client = new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failureCount, error) => {
          const status = parseApiError(error).status
          if (status === 401 || status === 403) return false
          return failureCount < 2
        },
        refetchOnWindowFocus: import.meta.env.PROD,
        staleTime: 10_000,
      },
    },
    queryCache: new QueryCache({
      onError: (error) => handleGlobalError(error, client),
    }),
    mutationCache: new MutationCache({
      onError: (error) => handleGlobalError(error, client),
    }),
  })
  return client
}

export const queryClient = createAppQueryClient()
