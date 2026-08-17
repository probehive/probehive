import { screen, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import type { OrganizationOverviewResponse } from '../api/overview'
import { jsonResponse, mockFetchRoutes, renderWithProviders } from '../test/renderWithProviders.tsx'
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

const overview: OrganizationOverviewResponse = {
  organizationId: organization.id,
  monitors: { total: 0, draft: 0, active: 0, paused: 0, archived: 0 },
  health: { notEvaluated: 0, unknown: 0, healthy: 0, degraded: 0, down: 0 },
  incidents: {
    active: 0,
    open: 0,
    acknowledged: 0,
    activePreview: [],
    activePreviewTruncated: false,
  },
  integrations: { total: 0, enabled: 0 },
  statusPage: { configured: false, published: false },
  capabilities: {
    manageOrganization: true,
    manageIntegrations: true,
    manageStatusPage: true,
  },
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage(organizationId: string) {
  renderWithProviders(<OrganizationPage />, 'organizations/:organizationId', `/organizations/${organizationId}`)
}

test('renders the organization and its default project', async () => {
  const monitorURL = `/api/v1/organizations/${organization.id}/projects/${organization.defaultProject.id}/monitors`
  const fetchMock = mockFetchRoutes({
    [`/api/v1/organizations/${organization.id}/status-page/draft`]: () => new Response(null, { status: 204 }),
    [monitorURL]: () => jsonResponse(200, []),
    [`/api/v1/organizations/${organization.id}/overview`]: () => jsonResponse(200, overview),
    [`/api/v1/organizations/${organization.id}`]: () => jsonResponse(200, organization),
  })
  renderPage(organization.id)

  expect(await screen.findByRole('heading', { name: 'Acme Monitoring' })).toBeInTheDocument()
  expect(screen.getByText('acme')).toBeInTheDocument()
  expect(screen.getByText('Default')).toBeInTheDocument()
  expect(await screen.findByText('No Monitors yet.')).toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Operational overview' })).toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Organization settings' })).toBeInTheDocument()
  expect(within(screen.getByRole('navigation', { name: 'Organization sections' }))
    .getByRole('link', { name: 'Webhook integrations' })).toHaveAttribute(
    'href',
    '/organizations/' + organization.id + '/integrations',
  )
  expect(fetchMock).toHaveBeenCalledWith(`/api/v1/organizations/${organization.id}`)
  expect(fetchMock).toHaveBeenCalledWith(monitorURL)
})

test('reports an unknown organization', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(404, { status: 404 }))
  renderPage('019f8f3d-0000-0000-0000-000000000000')

  expect(await screen.findByText('This Organization does not exist.')).toBeInTheDocument()
})
