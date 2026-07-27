import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import { jsonResponse, mockFetchRoutes, renderWithProviders } from '../test/renderWithProviders.tsx'
import OrganizationsPage from './OrganizationsPage.tsx'

const organization: OrganizationResponse = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  slug: 'acme',
  displayName: 'Acme Monitoring',
  createdAt: '2026-07-23T12:00:00+00:00',
  defaultProject: {
    id: '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420',
    organizationId: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
    name: 'Default',
    isDefault: true,
    createdAt: '2026-07-23T12:00:00+00:00',
  },
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage() {
  renderWithProviders(<OrganizationsPage />, '', '/')
}

async function submit(slug: string, displayName: string) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Slug'), slug)
  await user.type(screen.getByLabelText('Display name'), displayName)
  await user.click(screen.getByRole('button', { name: 'Create' }))
}

test('lists the Organizations the installation already has', async () => {
  mockFetchRoutes({ '/api/v1/organizations': () => jsonResponse(200, [organization]) })
  renderPage()

  expect(await screen.findByRole('link', { name: 'Acme Monitoring' })).toHaveAttribute(
    'href',
    `/organizations/${organization.id}`,
  )
})

test('reports an installation with no Organizations', async () => {
  mockFetchRoutes({ '/api/v1/organizations': () => jsonResponse(200, []) })
  renderPage()

  expect(await screen.findByText('No Organizations yet.')).toBeInTheDocument()
})

test('renders the provisioning form for additional Organizations', async () => {
  mockFetchRoutes({ '/api/v1/organizations': () => jsonResponse(200, [organization]) })
  renderPage()

  expect(await screen.findByRole('form', { name: 'Create organization' })).toBeInTheDocument()
  expect(screen.getByLabelText('Slug')).toBeInTheDocument()
  expect(screen.getByLabelText('Display name')).toBeInTheDocument()
})

test('shows a success message with a link after creating', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL, init) => {
    if (init?.method === 'POST') {
      return Promise.resolve(jsonResponse(201, organization))
    }
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url.startsWith('/api/v1/organizations')) {
      return Promise.resolve(jsonResponse(200, []))
    }
    if (url.startsWith('/api/v1/auth/antiforgery')) {
      return Promise.resolve(
        jsonResponse(200, { headerName: 'X-ProbeHive-Antiforgery', requestToken: 'token' }),
      )
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage()

  await submit('acme', 'Acme Monitoring')

  expect(await screen.findByText('Organization created.')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'View Acme Monitoring' })).toHaveAttribute(
    'href',
    `/organizations/${organization.id}`,
  )
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/v1/organizations',
    expect.objectContaining({ method: 'POST' }),
  )
})

test('shows field-level validation problems', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input: RequestInfo | URL, init) =>
    Promise.resolve(
      init?.method === 'POST'
        ? jsonResponse(400, {
            title: 'One or more validation errors occurred.',
            status: 400,
            errors: {
              slug: [{ code: 'organization.slug.invalid', message: 'A slug is 3 to 63 characters.' }],
              displayName: [
                {
                  code: 'organization.displayName.invalid',
                  message: 'A display name is 1 to 100 characters after trimming.',
                },
              ],
            },
          })
        : jsonResponse(200, []),
    ),
  )
  renderPage()

  await submit('x', ' ')

  expect(await screen.findByText('A slug is 3 to 63 characters.')).toBeInTheDocument()
  expect(screen.getByText('A display name is 1 to 100 characters after trimming.')).toBeInTheDocument()
})

test('shows a conflict message when the slug is taken', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input: RequestInfo | URL, init) =>
    Promise.resolve(
      init?.method === 'POST'
        ? jsonResponse(409, { title: 'Organization slug already in use', status: 409 })
        : jsonResponse(200, []),
    ),
  )
  renderPage()

  await submit('acme', 'Another Company')

  expect(await screen.findByText(/already in use/)).toBeInTheDocument()
})
