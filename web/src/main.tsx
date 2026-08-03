import { QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import '@fontsource-variable/geist'
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import { shouldCoordinateUnauthorized } from '@/features/auth/navigation'
import '@/lib/i18n' // i18next 初始化（同步完成，保证首帧即翻译）
import { queryClient, setUnauthorizedHandler } from '@/lib/query-client'
import { DirectionProvider } from './context/direction-provider'
import { ThemeProvider } from './context/theme-provider'
// Generated Routes
import { routeTree } from './routeTree.gen'
// Styles
import './styles/index.css'

// Create a new router instance
const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
})

setUnauthorizedHandler(() => {
  if (!shouldCoordinateUnauthorized(router.history.location.pathname)) {
    return false
  }
  const redirect = `${router.history.location.href}`
  void router.navigate({ to: '/sign-in', search: { redirect } })
  return true
})

// Register the router instance for type safety
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// Render the app
const rootElement = document.getElementById('root')!
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement)
  root.render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <DirectionProvider>
            <RouterProvider router={router} />
          </DirectionProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </StrictMode>
  )
}
