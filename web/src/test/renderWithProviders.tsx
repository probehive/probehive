import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router'
import { vi } from 'vitest'

import type { SessionResponse } from '../api/auth.ts'
import App from '../App.tsx'
import { sessionQueryKey } from '../auth/useSession.ts'

interface RenderOptions {
  /** Seeded session so the App shell does not fetch one; defaults to signed out. */
  session?: SessionResponse | null
}

export function renderRoutes(children: RouteObject[], initialPath: string, options: RenderOptions = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  queryClient.setQueryData(sessionQueryKey, options.session ?? null)
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <App />,
        children,
      },
    ],
    { initialEntries: [initialPath] },
  )
  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
    queryClient,
  }
}

export function renderWithProviders(element: ReactElement, path: string, initialPath: string, options: RenderOptions = {}) {
  return renderRoutes([path === '' ? { index: true, element } : { path, element }], initialPath, options)
}

export function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json' },
  })
}

/** Routes fetch calls by URL prefix; each handler builds a fresh Response per call. */
export function mockFetchRoutes(routes: Record<string, () => Response>) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    for (const [prefix, makeResponse] of Object.entries(routes)) {
      if (url.startsWith(prefix)) {
        return Promise.resolve(makeResponse())
      }
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
}
