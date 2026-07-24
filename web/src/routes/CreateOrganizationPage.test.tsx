import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import CreateOrganizationPage from './CreateOrganizationPage.tsx'

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
  renderWithProviders(<CreateOrganizationPage />, '', '/')
}

async function submit(slug: string, displayName: string) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Slug'), slug)
  await user.type(screen.getByLabelText('Display name'), displayName)
  await user.click(screen.getByRole('button', { name: 'Create' }))
}

test('renders the provisioning form', () => {
  renderPage()

  expect(screen.getByRole('form', { name: 'Create organization' })).toBeInTheDocument()
  expect(screen.getByLabelText('Slug')).toBeInTheDocument()
  expect(screen.getByLabelText('Display name')).toBeInTheDocument()
})

test('shows a success message with a link after creating', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(201, organization))
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

test('reports an idempotent replay distinctly', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, organization))
  renderPage()

  await submit('acme', 'Acme Monitoring')

  expect(await screen.findByText(/already existed/)).toBeInTheDocument()
})

test('shows field-level validation problems', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    jsonResponse(400, {
      title: 'One or more validation errors occurred.',
      status: 400,
      errors: {
        slug: ['A slug is 3 to 63 characters.'],
        displayName: ['A display name is 1 to 100 characters after trimming.'],
      },
    }),
  )
  renderPage()

  await submit('x', ' ')

  expect(await screen.findByText('A slug is 3 to 63 characters.')).toBeInTheDocument()
  expect(screen.getByText('A display name is 1 to 100 characters after trimming.')).toBeInTheDocument()
})

test('shows a conflict message when the slug is taken', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    jsonResponse(409, { title: 'Organization slug already in use', status: 409 }),
  )
  renderPage()

  await submit('acme', 'Another Company')

  expect(await screen.findByText(/already in use/)).toBeInTheDocument()
})
