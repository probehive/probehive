import { screen, within } from '@testing-library/react'
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

test('edits a user profile inline and refreshes the list', async () => {
  const updated = { ...teammate, email: 'renamed@example.test', displayName: 'Renamed' }
  let listCalls = 0
  const putUrls: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL, init) => {
    if (init?.method === 'PUT') {
      putUrls.push(String(input))
      return Promise.resolve(jsonResponse(200, updated))
    }
    listCalls++
    return Promise.resolve(jsonResponse(200, listCalls === 1 ? [administrator, teammate] : [administrator, updated]))
  })
  renderPage()

  const user = userEvent.setup()
  const editButtons = await screen.findAllByRole('button', { name: 'Edit' })
  const teammateEdit = editButtons[1]
  if (!teammateEdit) {
    throw new Error('teammate edit button missing')
  }
  await user.click(teammateEdit)

  const editForm = screen.getByRole('form', { name: 'Edit user' })
  await user.clear(within(editForm).getByLabelText('Email'))
  await user.type(within(editForm).getByLabelText('Email'), 'renamed@example.test')
  await user.clear(within(editForm).getByLabelText('Display name (optional)'))
  await user.type(within(editForm).getByLabelText('Display name (optional)'), 'Renamed')
  await user.click(within(editForm).getByRole('button', { name: 'Save' }))

  expect(await screen.findByText('Renamed')).toBeInTheDocument()
  expect(screen.getByText('renamed@example.test')).toBeInTheDocument()
  expect(putUrls).toEqual([`/api/v1/users/${teammate.id}`])
  expect(screen.queryByRole('form', { name: 'Edit user' })).not.toBeInTheDocument()
})

test('shows a conflict message when editing to an email already in use', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input: RequestInfo | URL, init) =>
    Promise.resolve(
      init?.method === 'PUT'
        ? jsonResponse(409, {
            title: 'Email already in use',
            status: 409,
            code: 'user.email.conflict',
            detail: 'A user with this email address already exists.',
          })
        : jsonResponse(200, [administrator, teammate]),
    ),
  )
  renderPage()

  const user = userEvent.setup()
  const editButtons = await screen.findAllByRole('button', { name: 'Edit' })
  const teammateEdit = editButtons[1]
  if (!teammateEdit) {
    throw new Error('teammate edit button missing')
  }
  await user.click(teammateEdit)

  const editForm = screen.getByRole('form', { name: 'Edit user' })
  await user.click(within(editForm).getByRole('button', { name: 'Save' }))

  expect(await screen.findByText('A user with this email address already exists.')).toBeInTheDocument()
})
