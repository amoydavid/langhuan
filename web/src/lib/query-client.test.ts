import { MutationObserver } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from './api/error'
import {
  createAppQueryClient,
  handleUnauthorizedOnce,
  queryClient,
  resetUnauthorizedNavigation,
  setUnauthorizedHandler,
} from './query-client'

const toastError = vi.hoisted(() => vi.fn())

vi.mock('sonner', () => ({
  toast: {
    error: toastError,
  },
}))

beforeEach(() => {
  queryClient.clear()
  resetUnauthorizedNavigation()
  setUnauthorizedHandler(undefined)
  toastError.mockClear()
})

describe('unauthorized coordination', () => {
  it('clears cached server state and navigates only once', () => {
    const navigate = vi.fn()
    queryClient.setQueryData(['cached'], { secret: true })
    setUnauthorizedHandler(navigate)

    handleUnauthorizedOnce()
    handleUnauthorizedOnce()

    expect(queryClient.getQueryData(['cached'])).toBeUndefined()
    expect(navigate).toHaveBeenCalledOnce()
    expect(toastError).toHaveBeenCalledOnce()
    expect(toastError).toHaveBeenCalledWith('登录已过期，请重新登录')
  })

  it('allows a later authenticated session to reset the gate', () => {
    const navigate = vi.fn()
    setUnauthorizedHandler(navigate)

    handleUnauthorizedOnce()
    resetUnauthorizedNavigation()
    handleUnauthorizedOnce()

    expect(navigate).toHaveBeenCalledTimes(2)
  })

  it('coordinates concurrent 401 query failures through the global gate', async () => {
    const client = createAppQueryClient()
    const navigate = vi.fn()
    setUnauthorizedHandler(navigate)
    client.setQueryData(['cached'], { secret: true })

    await Promise.allSettled(
      ['first', 'second'].map((key) =>
        client.fetchQuery({
          queryKey: [key],
          queryFn: async () => {
            throw new ApiError('未登录', 401, 'unauthorized')
          },
        })
      )
    )

    expect(client.getQueryData(['cached'])).toBeUndefined()
    expect(navigate).toHaveBeenCalledOnce()
    expect(toastError).toHaveBeenCalledOnce()
  })

  it('leaves expected public-route 401 responses to the route guard', () => {
    const navigate = vi.fn(() => false)
    queryClient.setQueryData(['bootstrap-status'], { initialized: false })
    setUnauthorizedHandler(navigate)

    handleUnauthorizedOnce()

    expect(navigate).toHaveBeenCalledOnce()
    expect(queryClient.getQueryData(['bootstrap-status'])).toEqual({
      initialized: false,
    })
    expect(toastError).not.toHaveBeenCalled()
  })
})

describe('query and mutation error policy', () => {
  it('does not retry forbidden queries and refreshes the me summary', async () => {
    const client = createAppQueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    let attempts = 0

    await expect(
      client.fetchQuery({
        queryKey: ['members', 'acme'],
        queryFn: async () => {
          attempts += 1
          throw new ApiError('权限不足', 403, 'forbidden')
        },
      })
    ).rejects.toMatchObject({ status: 403 })

    expect(attempts).toBe(1)
    expect(toastError).toHaveBeenCalledWith('权限不足')
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['me'] })
  })

  it.each([404, 409, 413, 415, 429, 500])(
    'leaves mutation state to the feature for HTTP %s',
    async (status) => {
      const client = createAppQueryClient()
      client.setQueryData(['form-context'], { preserved: true })
      const observer = new MutationObserver(client, {
        mutationFn: async () => {
          throw new ApiError('feature error', status, 'feature_error')
        },
      })

      await expect(observer.mutate()).rejects.toMatchObject({ status })

      expect(client.getQueryData(['form-context'])).toEqual({ preserved: true })
      expect(toastError).not.toHaveBeenCalled()
    }
  )

  it('lets server query failures reach the caller without global navigation', async () => {
    const client = createAppQueryClient()

    await expect(
      client.fetchQuery({
        queryKey: ['route-initialization'],
        queryFn: async () => {
          throw new ApiError('服务异常', 500, 'internal_error')
        },
      })
    ).rejects.toMatchObject({ status: 500 })

    expect(toastError).not.toHaveBeenCalled()
  })
})
