import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { jsonResponse, mockFetchRoutes, renderRoutes } from '../test/renderWithProviders.tsx'
import UsersPage from './UsersPage.tsx'

const administrator = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  email: 'admin@example.test',
  displayName: 'admin@example.test',
  role: 'Administrator',
  createdAt: '2026-07-24T12:00:00+00:00',
}

const teammate = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ef',
  email: 'teammate@example.test',
  displayName: 'Teammate',
  role: 'Administrator',
  createdAt: '2026-07-25T12:00:00+00:00',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage() {
  return renderRoutes(
    [
      { path: 'users', element: <UsersPage /> },
      { path: 'login', element: <p>Login page</p> },
    ],
    '/users',
  )
}

test('lists the instance users in creation order with the team hint', async () => {
  mockFetchRoutes({
    '/api/v1/users': () => jsonResponse(200, [administrator, teammate]),
  })
  renderPage()

  expect(await screen.findByText('teammate@example.test')).toBeInTheDocument()
  expect(screen.getByText('Teammate')).toBeInTheDocument()
  expect(screen.getByText(/An instance is one team/)).toBeInTheDocument()
})

test('creates a user with an optional display name and refreshes the list', async () => {
  mockFetchRoutes({
    '/api/v1/users': () => jsonResponse(200, [administrator, teammate]),
  })
  renderPage()

  const user = userEvent.setup()
  await user.type(await screen.findByLabelText('Email'), 'TEAMMATE@Example.Test')
  await user.type(screen.getByLabelText('Initial password'), 'a-long-teammate-password')
  await user.click(screen.getByRole('button', { name: 'Create' }))

  expect(await screen.findByText('User created.')).toBeInTheDocument()
})

test('shows a conflict message when the email is already in use', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input: RequestInfo | URL, init) =>
    Promise.resolve(
      init?.method === 'POST'
        ? jsonResponse(409, {
            title: 'Email already in use',
            status: 409,
            code: 'user.email.conflict',
            detail: 'A user with this email address already exists.',
          })
        : jsonResponse(200, [administrator]),
    ),
  )
  renderPage()

  const user = userEvent.setup()
  await user.type(await screen.findByLabelText('Email'), 'admin@example.test')
  await user.type(screen.getByLabelText('Initial password'), 'a-long-teammate-password')
  await user.click(screen.getByRole('button', { name: 'Create' }))

  expect(await screen.findByText('A user with this email address already exists.')).toBeInTheDocument()
})
