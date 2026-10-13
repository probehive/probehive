import { screen, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import type { OrganizationOverviewResponse } from '../api/overview'
import { jsonResponse, mockFetchRoutes, renderRoutes } from '../test/renderWithProviders.tsx'
import IntegrationsPage from './IntegrationsPage.tsx'
import OrganizationIncidentsPage from './OrganizationIncidentsPage.tsx'
import OrganizationLayout from './OrganizationLayout.tsx'
import OrganizationMonitorsPage from './OrganizationMonitorsPage.tsx'
import OrganizationOverviewPage from './OrganizationOverviewPage.tsx'
import OrganizationSettingsPage from './OrganizationSettingsPage.tsx'
import OrganizationStatusPage from './OrganizationStatusPage.tsx'

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

function mockWorkspaceRoutes() {
  const inventoryURL = `/api/v1/organizations/${organization.id}/monitor-inventory`
  const monitorURL = `/api/v1/organizations/${organization.id}/${organization.defaultProject.id}/monitors`
  const incidentsURL = `/api/v1/organizations/${organization.id}/incidents`
  const integrationsURL = `/api/v1/organizations/${organization.id}/webhook-integrations`
  return {
    fetchMock: mockFetchRoutes({
      [`/api/v1/organizations/${organization.id}/status-page/draft`]: () => new Response(null, { status: 204 }),
      [inventoryURL]: () => jsonResponse(200, { items: [], page: 1, pageSize: 10, total: 0 }),
      [monitorURL]: () => jsonResponse(200, []),
      [`${incidentsURL}?`]: () => jsonResponse(200, { items: [], nextCursor: null }),
      [incidentsURL]: () => jsonResponse(200, { items: [], nextCursor: null }),
      [`${integrationsURL}?`]: () => jsonResponse(200, []),
      [integrationsURL]: () => jsonResponse(200, []),
      [`/api/v1/organizations/${organization.id}/overview`]: () => jsonResponse(200, overview),
      [`/api/v1/organizations/${organization.id}`]: () => jsonResponse(200, organization),
    }),
    inventoryURL,
  }
}

function renderWorkspace(initialPath: string) {
  return renderRoutes(
    [
      {
        path: 'organizations/:organizationId',
        element: <OrganizationLayout />,
        children: [
          { index: true, element: <OrganizationOverviewPage /> },
          { path: 'monitors', element: <OrganizationMonitorsPage /> },
          { path: 'incidents', element: <OrganizationIncidentsPage /> },
          { path: 'integrations', element: <IntegrationsPage /> },
          { path: 'status', element: <OrganizationStatusPage /> },
          { path: 'settings', element: <OrganizationSettingsPage /> },
        ],
      },
    ],
    initialPath,
  )
}

function workspaceNavigation() {
  return screen.getByRole('navigation', { name: 'Organization sections' })
}

test('renders the organization frame with one stable URL per workspace section', async () => {
  mockWorkspaceRoutes()
  renderWorkspace(`/organizations/${organization.id}`)

  expect(await screen.findByRole('heading', { name: 'Acme Monitoring' })).toBeInTheDocument()
  const navigation = workspaceNavigation()
  const base = '/organizations/' + organization.id
  const expectedTabs: Array<[string, string]> = [
    ['Overview', base],
    ['Monitors', base + '/monitors'],
    ['Incidents', base + '/incidents'],
    ['Integrations', base + '/integrations'],
    ['Status', base + '/status'],
    ['Settings', base + '/settings'],
  ]
  for (const [name, href] of expectedTabs) {
    expect(within(navigation).getByRole('link', { name })).toHaveAttribute('href', href)
  }
  expect(within(navigation).getByRole('link', { name: 'Overview' })).toHaveAttribute(
    'aria-current',
    'page',
  )
})

test('renders the operational overview on the index route', async () => {
  mockWorkspaceRoutes()
  renderWorkspace(`/organizations/${organization.id}`)

  expect(await screen.findByRole('region', { name: 'Operational overview' })).toBeInTheDocument()
  expect(await screen.findByRole('region', { name: 'Monitoring' })).toBeInTheDocument()
})

test('renders the monitor inventory on the monitors route', async () => {
  const { fetchMock, inventoryURL } = mockWorkspaceRoutes()
  renderWorkspace(`/organizations/${organization.id}/monitors`)

  expect(await screen.findByRole('region', { name: 'Monitors' })).toBeInTheDocument()
  expect(within(workspaceNavigation()).getByRole('link', { name: 'Monitors' }))
    .toHaveAttribute('aria-current', 'page')
  expect(fetchMock).toHaveBeenCalledWith(inventoryURL + '?sort=name&direction=asc&page=1&pageSize=10')
})

test('renders the status page draft on the status route', async () => {
  mockWorkspaceRoutes()
  renderWorkspace(`/organizations/${organization.id}/status`)

  expect(await screen.findByRole('region', { name: 'Status page draft' })).toBeInTheDocument()
  expect(within(workspaceNavigation()).getByRole('link', { name: 'Status' }))
    .toHaveAttribute('aria-current', 'page')
})

test('renders consolidated organization settings on the settings route', async () => {
  mockWorkspaceRoutes()
  renderWorkspace(`/organizations/${organization.id}/settings`)

  expect(await screen.findByRole('region', { name: 'Organization settings' })).toBeInTheDocument()
  expect(within(workspaceNavigation()).getByRole('link', { name: 'Settings' }))
    .toHaveAttribute('aria-current', 'page')
})

test('reports an unknown organization', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(404, { status: 404 }))
  renderWorkspace('/organizations/019f8f3d-0000-0000-0000-000000000000')

  expect(await screen.findByText('This Organization does not exist.')).toBeInTheDocument()
})
