import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { jsonResponse, mockFetchRoutes, renderRoutes } from '../test/renderWithProviders.tsx'
import SetupPage from './SetupPage.tsx'

const administrator = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  email: 'admin@example.test',
  displayName: 'First Administrator',
  role: 'Administrator',
  createdAt: '2026-07-24T12:00:00+00:00',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage() {
  return renderRoutes(
    [
      { index: true, element: <p>Home page</p> },
      { path: 'setup', element: <SetupPage /> },
      { path: 'login', element: <p>Login page</p> },
    ],
    '/setup',
  )
}

async function submit() {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Email'), 'admin@example.test')
  await user.type(screen.getByLabelText('Display name'), 'First Administrator')
  await user.type(screen.getByLabelText('Password'), 'a-long-admin-password')
  await user.click(screen.getByRole('button', { name: 'Create administrator' }))
}

test('creates the first administrator and lands on the home page', async () => {
  mockFetchRoutes({
    '/api/v1/setup/status': () => jsonResponse(200, { setupComplete: false }),
    '/api/v1/setup/admin': () => jsonResponse(201, administrator),
    '/api/v1/auth/antiforgery': () =>
      jsonResponse(200, { headerName: 'X-ProbeHive-Antiforgery', requestToken: 'fresh-token' }),
    '/api/v1/auth/session': () => jsonResponse(200, {
      userId: administrator.id,
      email: administrator.email,
      displayName: administrator.displayName,
      role: administrator.role,
    }),
  })
  renderPage()

  await submit()

  expect(await screen.findByText('Home page')).toBeInTheDocument()
})

test('shows field-level validation problems', async () => {
  mockFetchRoutes({
    '/api/v1/setup/status': () => jsonResponse(200, { setupComplete: false }),
    '/api/v1/setup/admin': () =>
      jsonResponse(400, {
        title: 'One or more validation errors occurred.',
        status: 400,
        errors: {
          password: ['A password is 12 to 128 characters.'],
        },
      }),
  })
  renderPage()

  await submit()

  expect(await screen.findByText('A password is 12 to 128 characters.')).toBeInTheDocument()
})

test('redirects to login once setup is complete', async () => {
  mockFetchRoutes({
    '/api/v1/setup/status': () => jsonResponse(200, { setupComplete: true }),
  })
  renderPage()

  expect(await screen.findByText('Login page')).toBeInTheDocument()
})
