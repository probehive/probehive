import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationOverviewResponse } from '../api/overview'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import OrganizationOverview from './OrganizationOverview.tsx'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'

const freshOverview: OrganizationOverviewResponse = {
  organizationId,
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

function renderOverview() {
  return renderWithProviders(
    <OrganizationOverview organizationId={organizationId} />,
    '',
    '/',
  )
}

test('shows loading and recovers from a load error through retry', async () => {
  let resolveFirst!: (response: Response) => void
  const firstResponse = new Promise<Response>((resolve) => {
    resolveFirst = resolve
  })
  const fetchMock = vi.spyOn(globalThis, 'fetch')
    .mockReturnValueOnce(firstResponse)
    .mockResolvedValueOnce(jsonResponse(200, freshOverview))

  renderOverview()
  expect(screen.getByText('Loading operational overview...')).toBeInTheDocument()

  resolveFirst(jsonResponse(500, { title: 'Internal Server Error', status: 500 }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'The operational overview could not be loaded.',
  )

  await userEvent.setup().click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('No Monitors yet. Create the first HTTP Monitor below.'))
    .toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledTimes(2)
})

test('renders an honest fresh Organization with administrator destinations', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, freshOverview))
  renderOverview()

  const overview = await screen.findByRole('region', { name: 'Operational overview' })
  expect(await within(overview).findByText('No Monitors yet. Create the first HTTP Monitor below.'))
    .toBeInTheDocument()
  expect(within(overview).getByText('No active Monitor health is being evaluated.'))
    .toBeInTheDocument()
  expect(within(overview).getByText('No active Incidents.')).toBeInTheDocument()
  expect(within(overview).getByText('No Webhook integrations configured')).toBeInTheDocument()
  expect(within(overview).getByText('Not configured')).toBeInTheDocument()
  expect(within(overview).getByRole('link', { name: 'View Monitors' }))
    .toHaveAttribute('href', '#monitors')
  expect(within(overview).getByRole('link', { name: 'View Incident inbox' }))
    .toHaveAttribute('href', '/organizations/' + organizationId + '/incidents')
  expect(within(overview).getByRole('link', { name: 'Webhook integrations' }))
    .toHaveAttribute('href', '/organizations/' + organizationId + '/integrations')
  expect(within(overview).getByRole('link', { name: 'Status page' }))
    .toHaveAttribute('href', '#status-page')
  expect(within(overview).getByRole('link', { name: 'Organization settings' }))
    .toHaveAttribute('href', '#organization-settings')
})

test('distinguishes inactive Monitors from unevaluated Active Monitors', async () => {
  const responses: OrganizationOverviewResponse[] = [
    {
      ...freshOverview,
      monitors: { total: 2, draft: 1, active: 0, paused: 1, archived: 0 },
    },
    {
      ...freshOverview,
      monitors: { total: 3, draft: 0, active: 2, paused: 1, archived: 0 },
      health: { notEvaluated: 2, unknown: 0, healthy: 0, degraded: 0, down: 0 },
    },
  ]
  vi.spyOn(globalThis, 'fetch')
    .mockResolvedValueOnce(jsonResponse(200, responses[0]))
    .mockResolvedValueOnce(jsonResponse(200, responses[1]))

  const first = renderOverview()
  expect(await screen.findByText(
    'No active Monitors. Health evaluation resumes when a Monitor is active.',
  )).toBeInTheDocument()
  first.unmount()

  renderOverview()
  expect(await screen.findByText('Active Monitors awaiting their first evaluation: 2.'))
    .toBeInTheDocument()
})

test('renders populated counts and direct active Incident evidence links', async () => {
  const populated: OrganizationOverviewResponse = {
    ...freshOverview,
    monitors: { total: 5, draft: 1, active: 3, paused: 0, archived: 1 },
    health: { notEvaluated: 1, unknown: 0, healthy: 1, degraded: 0, down: 1 },
    incidents: {
      active: 2,
      open: 1,
      acknowledged: 1,
      activePreview: [
        {
          id: '00000000-0000-7000-8000-000000000101',
          projectId: '00000000-0000-7000-8000-000000000201',
          monitorId: '00000000-0000-7000-8000-000000000301',
          monitorName: 'Checkout',
          state: 'open',
          updatedAt: '2026-08-17T02:00:00Z',
        },
      ],
      activePreviewTruncated: true,
    },
    integrations: { total: 2, enabled: 1 },
    statusPage: { configured: true, published: true },
  }
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, populated))
  renderOverview()

  const overview = await screen.findByRole('region', { name: 'Operational overview' })
  const totalLabel = await within(overview).findByText('Total Monitors')
  expect(within(totalLabel.parentElement!).getByText('5'))
    .toBeInTheDocument()
  expect(within(overview).getByText('Active Monitors awaiting their first evaluation: 1.'))
    .toBeInTheDocument()
  expect(within(overview).getByRole('link', { name: 'View Incident evidence for Checkout' }))
    .toHaveAttribute(
      'href',
      '/organizations/' + organizationId +
        '/projects/00000000-0000-7000-8000-000000000201' +
        '/monitors/00000000-0000-7000-8000-000000000301' +
        '/incidents/00000000-0000-7000-8000-000000000101',
    )
  expect(within(overview).getByRole('link', { name: 'View all active Incidents in the inbox.' }))
    .toHaveAttribute('href', '/organizations/' + organizationId + '/incidents')
  expect(within(overview).getByText('1 of 2 enabled')).toBeInTheDocument()
  expect(within(overview).getByText('Published')).toBeInTheDocument()
})

test('keeps administrator-only state unavailable to a Viewer', async () => {
  const viewerOverview: OrganizationOverviewResponse = {
    ...freshOverview,
    integrations: null,
    statusPage: null,
    capabilities: {
      manageOrganization: false,
      manageIntegrations: false,
      manageStatusPage: false,
    },
  }
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, viewerOverview))
  renderOverview()

  const overview = await screen.findByRole('region', { name: 'Operational overview' })
  expect(await within(overview).findByText('Administrative state is unavailable for your role.'))
    .toBeInTheDocument()
  expect(within(overview).queryByRole('link', { name: 'Webhook integrations' }))
    .not.toBeInTheDocument()
  expect(within(overview).queryByRole('link', { name: 'Organization settings' }))
    .not.toBeInTheDocument()
})
