import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { jsonResponse, mockFetchRoutes, renderRoutes } from '../test/renderWithProviders.tsx'
import RequireAuth from './RequireAuth.tsx'

const session = {
  userId: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  email: 'admin@example.test',
  displayName: 'First Administrator',
  role: 'Administrator',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderGuarded(sessionValue: typeof session | null) {
  return renderRoutes(
    [
      { path: 'login', element: <p>Login page</p> },
      {
        element: <RequireAuth />,
        children: [{ index: true, element: <p>Protected page</p> }],
      },
    ],
    '/',
    { session: sessionValue },
  )
}

test('redirects to login without a session', async () => {
  renderGuarded(null)

  expect(await screen.findByText('Login page')).toBeInTheDocument()
  expect(screen.queryByText('Protected page')).not.toBeInTheDocument()
})

test('renders the protected page with a session and shows the signed-in user', async () => {
  renderGuarded(session)

  expect(await screen.findByText('Protected page')).toBeInTheDocument()
  expect(screen.getByText('admin@example.test')).toBeInTheDocument()
})

test('signing out posts the logout request and returns to login', async () => {
  const fetchMock = mockFetchRoutes({
    '/api/v1/auth/logout': () => new Response(null, { status: 204 }),
    '/api/v1/auth/antiforgery': () =>
      jsonResponse(200, { headerName: 'X-ProbeHive-Antiforgery', requestToken: 'fresh-token' }),
  })
  renderGuarded(session)

  await userEvent.setup().click(await screen.findByRole('button', { name: 'Sign out' }))

  expect(await screen.findByText('Login page')).toBeInTheDocument()
  expect(fetchMock.mock.calls.some(([input]) => input === '/api/v1/auth/logout')).toBe(true)
})
