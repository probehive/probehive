import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { jsonResponse, mockFetchRoutes, renderRoutes } from '../test/renderWithProviders.tsx'
import LoginPage from './LoginPage.tsx'

const session = {
  userId: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  email: 'admin@example.test',
  displayName: 'First Administrator',
  role: 'Administrator',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage() {
  return renderRoutes(
    [
      { index: true, element: <p>Home page</p> },
      { path: 'login', element: <LoginPage /> },
      { path: 'setup', element: <p>Setup page</p> },
    ],
    '/login',
  )
}

async function submit(email: string, password: string) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Email'), email)
  await user.type(screen.getByLabelText('Password'), password)
  await user.click(screen.getByRole('button', { name: 'Sign in' }))
}

test('signs in and navigates to the home page', async () => {
  const fetchMock = mockFetchRoutes({
    '/api/v1/setup/status': () => jsonResponse(200, { setupComplete: true }),
    '/api/v1/auth/login': () => jsonResponse(200, session),
    '/api/v1/auth/antiforgery': () =>
      jsonResponse(200, { headerName: 'X-ProbeHive-Antiforgery', requestToken: 'fresh-token' }),
  })
  renderPage()

  await submit('admin@example.test', 'a-long-admin-password')

  expect(await screen.findByText('Home page')).toBeInTheDocument()
  const loginCall = fetchMock.mock.calls.find(([input]) => input === '/api/v1/auth/login')
  expect(loginCall).toBeDefined()
  const headers = (loginCall![1]!.headers ?? {}) as Record<string, string>
  expect(headers['X-ProbeHive-Antiforgery']).toBe('test-token')
})

test('shows a generic message for invalid credentials', async () => {
  mockFetchRoutes({
    '/api/v1/setup/status': () => jsonResponse(200, { setupComplete: true }),
    '/api/v1/auth/login': () =>
      jsonResponse(401, { title: 'Invalid credentials', status: 401 }),
  })
  renderPage()

  await submit('admin@example.test', 'wrong-password-entirely')

  expect(await screen.findByRole('alert')).toHaveTextContent('Invalid email or password.')
})

test('redirects to setup while the instance has no users', async () => {
  mockFetchRoutes({
    '/api/v1/setup/status': () => jsonResponse(200, { setupComplete: false }),
  })
  renderPage()

  expect(await screen.findByText('Setup page')).toBeInTheDocument()
})
