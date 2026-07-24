import { screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import OrganizationPage from './OrganizationPage.tsx'

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

function renderPage(organizationId: string) {
  renderWithProviders(<OrganizationPage />, 'organizations/:organizationId', `/organizations/${organizationId}`)
}

test('renders the organization and its default project', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, organization))
  renderPage(organization.id)

  expect(await screen.findByRole('heading', { name: 'Acme Monitoring' })).toBeInTheDocument()
  expect(screen.getByText('acme')).toBeInTheDocument()
  expect(screen.getByText('Default')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(`/api/v1/organizations/${organization.id}`)
})

test('reports an unknown organization', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(404, { status: 404 }))
  renderPage('019f8f3d-0000-0000-0000-000000000000')

  expect(await screen.findByText('This Organization does not exist.')).toBeInTheDocument()
})
