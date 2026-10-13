import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import { en } from '../i18n/en'
import { jsonResponse, mockFetchRoutes, renderWithProviders } from '../test/renderWithProviders.tsx'
import OrganizationsPage from './OrganizationsPage.tsx'

const organization: OrganizationResponse = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
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

async function submit(displayName: string) {
  const user = userEvent.setup()
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
  expect(screen.getByLabelText('Display name')).toBeInTheDocument()
})

test('shows a success message after creating', async () => {
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

  await submit('Acme Monitoring')

  expect(await screen.findByText('Organization created.')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/v1/organizations',
    expect.objectContaining({ method: 'POST' }),
  )
})

test('localizes field-level failures from their code, not the server sentence', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input: RequestInfo | URL, init) =>
    Promise.resolve(
      init?.method === 'POST'
        ? jsonResponse(400, {
            title: 'One or more validation errors occurred.',
            status: 400,
            errors: {
              // A deliberately different sentence: the catalog entry must win.
              displayName: [
                {
                  code: 'organization.displayName.invalid',
                  message: 'server wording that must not appear',
                },
              ],
            },
          })
        : jsonResponse(200, []),
    ),
  )
  renderPage()

  await submit(' ')

  expect(await screen.findByText(en['error.organization.displayName.invalid'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})

test('falls back to the server message for a code the catalog does not know', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input: RequestInfo | URL, init) =>
    Promise.resolve(
      init?.method === 'POST'
        ? jsonResponse(400, {
            title: 'One or more validation errors occurred.',
            status: 400,
            errors: {
              displayName: [
                { code: 'organization.displayName.inventedByANewerServer', message: 'A newer rule failed.' },
              ],
            },
          })
        : jsonResponse(200, []),
    ),
  )
  renderPage()

  await submit('Acme')

  expect(await screen.findByText('A newer rule failed.')).toBeInTheDocument()
})
