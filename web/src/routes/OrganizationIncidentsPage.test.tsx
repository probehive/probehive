import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useLocation } from 'react-router'
import { afterEach, expect, test, vi } from 'vitest'

import type { IncidentInboxItemResponse, IncidentInboxPageResponse } from '../api/incidents'
import type { OrganizationResponse } from '../api/organizations'
import { jsonResponse, renderRoutes } from '../test/renderWithProviders.tsx'
import OrganizationIncidentsPage from './OrganizationIncidentsPage.tsx'

const organization: OrganizationResponse = {
  id: '00000000-0000-7000-8000-000000000001',
  slug: 'acme',
  displayName: 'Acme Monitoring',
  createdAt: '2026-08-18T00:00:00Z',
  defaultProject: {
    id: '00000000-0000-7000-8000-000000000002',
    organizationId: '00000000-0000-7000-8000-000000000001',
    name: 'Default',
    isDefault: true,
    createdAt: '2026-08-18T00:00:00Z',
  },
}

const organizationPath = '/api/v1/organizations/' + organization.id
const inboxPath = organizationPath + '/incidents'

function incidentItem(
  id: string,
  state: IncidentInboxItemResponse['incident']['state'],
  monitorName: string,
): IncidentInboxItemResponse {
  return {
    incident: {
      id,
      organizationId: organization.id,
      projectId: organization.defaultProject.id,
      monitorId: '00000000-0000-7000-8000-000000000010',
      state,
      version: 1,
      openedTransitionId: '00000000-0000-7000-8000-000000000020',
      acknowledgedBy: null,
      acknowledgedAt: null,
      resolvedTransitionId: null,
      resolvedAt: null,
      createdAt: '2026-08-18T01:00:00Z',
      updatedAt: '2026-08-18T01:05:00Z',
    },
    monitor: {
      id: '00000000-0000-7000-8000-000000000010',
      name: monitorName,
      state: 'active',
    },
    health: null,
    maintenance: null,
    openingRun: null,
  }
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location-search">{location.search}</output>
}

function renderInbox(initialSearch = '', locale = 'en') {
  return renderRoutes([
    {
      path: 'organizations/:organizationId/incidents',
      element: (
        <>
          <OrganizationIncidentsPage />
          <LocationProbe />
        </>
      ),
    },
  ], '/organizations/' + organization.id + '/incidents' + initialSearch, { locale })
}

function mockInbox(
  respond: (url: string) => IncidentInboxPageResponse | Response,
) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url === organizationPath) {
      return Promise.resolve(jsonResponse(200, organization))
    }
    if (url.startsWith(inboxPath + '?')) {
      const value = respond(url)
      return Promise.resolve(value instanceof Response ? value : jsonResponse(200, value))
    }
    return Promise.reject(new Error('Unexpected fetch: ' + url))
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

test('renders active Organization Incident facts and retained unavailable Run evidence', async () => {
  const item = incidentItem(
    '00000000-0000-7000-8000-000000000030',
    'open',
    'Checkout API',
  )
  item.health = { state: 'down', updatedAt: '2026-08-18T01:04:00Z' }
  item.maintenance = {
    id: '00000000-0000-7000-8000-000000000040',
    state: 'active',
    startsAt: '2026-08-18T00:30:00Z',
    endsAt: '2026-08-18T02:30:00Z',
  }
  item.openingRun = {
    id: '00000000-0000-7000-8000-000000000050',
    scheduledFor: '2026-08-18T00:59:00Z',
    available: false,
  }
  const fetchMock = mockInbox(() => ({ items: [item], nextCursor: null }))

  renderInbox()

  const inbox = await screen.findByRole('region', { name: 'Active and recent Incidents' })
  expect(within(inbox).getByRole('combobox', { name: 'Lifecycle filter' })).toHaveValue('active')
  expect(within(inbox).getByRole('link', { name: 'Checkout API' })).toHaveAttribute(
    'href',
    '/organizations/' + organization.id +
      '/projects/' + organization.defaultProject.id +
      '/monitors/00000000-0000-7000-8000-000000000010',
  )
  expect(inbox.querySelector('.incident-state[data-state="open"]')).toHaveTextContent('Open')
  expect(inbox.querySelector('.health-state[data-state="down"]')).toHaveTextContent('Down')
  expect(within(inbox).getByText('Active window')).toBeInTheDocument()
  expect(within(inbox).getByText('Run evidence expired')).toBeInTheDocument()
  expect(within(inbox).queryByRole('link', { name: 'View opening Run' })).not.toBeInTheDocument()
  expect(within(inbox).getByRole('link', { name: 'View Incident evidence' })).toHaveAttribute(
    'href',
    '/organizations/' + organization.id +
      '/projects/' + organization.defaultProject.id +
      '/monitors/00000000-0000-7000-8000-000000000010' +
      '/incidents/00000000-0000-7000-8000-000000000030',
  )
  expect(fetchMock).toHaveBeenCalledWith(inboxPath + '?pageSize=20&state=active')
})

test('persists lifecycle filters in the URL and loads exactly one next cursor page', async () => {
  const openItem = incidentItem(
    '00000000-0000-7000-8000-000000000031',
    'open',
    'Checkout API',
  )
  const acknowledgedItem = incidentItem(
    '00000000-0000-7000-8000-000000000032',
    'acknowledged',
    'Billing API',
  )
  acknowledgedItem.openingRun = {
    id: '00000000-0000-7000-8000-000000000052',
    scheduledFor: '2026-08-18T00:58:00Z',
    available: true,
  }
  const resolvedItem = incidentItem(
    '00000000-0000-7000-8000-000000000033',
    'resolved',
    'Search API',
  )
  const fetchMock = mockInbox((url) => {
    if (url.includes('state=resolved')) return { items: [resolvedItem], nextCursor: null }
    if (url.includes('cursor=cursor-1')) return { items: [acknowledgedItem], nextCursor: null }
    return { items: [openItem], nextCursor: 'cursor-1' }
  })
  const user = userEvent.setup()

  renderInbox()

  expect(await screen.findByRole('link', { name: 'Checkout API' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Load more Incidents' }))

  expect(await screen.findByRole('link', { name: 'Billing API' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Load more Incidents' })).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'View opening Run' })).toHaveAttribute(
    'href',
    '/organizations/' + organization.id +
      '/projects/' + organization.defaultProject.id +
      '/monitors/00000000-0000-7000-8000-000000000010' +
      '/runs/00000000-0000-7000-8000-000000000052',
  )

  await user.selectOptions(screen.getByRole('combobox', { name: 'Lifecycle filter' }), 'resolved')

  expect(await screen.findByRole('link', { name: 'Search API' })).toBeInTheDocument()
  expect(screen.getByTestId('location-search')).toHaveTextContent('?state=resolved')
  expect(fetchMock).toHaveBeenCalledWith(inboxPath + '?pageSize=20&state=active&cursor=cursor-1')
  expect(fetchMock).toHaveBeenCalledWith(inboxPath + '?pageSize=20&state=resolved')
})

test('recovers from an empty resolved filter by returning to the active inbox', async () => {
  const activeItem = incidentItem(
    '00000000-0000-7000-8000-000000000034',
    'open',
    'Recovered API',
  )
  mockInbox((url) => url.includes('state=resolved')
    ? { items: [], nextCursor: null }
    : { items: [activeItem], nextCursor: null })
  const user = userEvent.setup()

  renderInbox('?state=resolved')

  expect(await screen.findByText('No Incidents match this lifecycle filter.')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Show active Incidents' }))

  expect(await screen.findByRole('link', { name: 'Recovered API' })).toBeInTheDocument()
  expect(screen.getByTestId('location-search')).toHaveTextContent('')
})

test('renders permission failure and retries the inbox request', async () => {
  let calls = 0
  mockInbox(() => {
    calls += 1
    return calls === 1
      ? jsonResponse(403, { title: 'Forbidden', status: 403 })
      : { items: [], nextCursor: null }
  })
  const user = userEvent.setup()

  renderInbox()

  expect(await screen.findByRole('alert')).toHaveTextContent(
    'You do not have permission to view this Incident inbox.',
  )
  await user.click(screen.getByRole('button', { name: 'Retry' }))

  expect(await screen.findByText('No Incidents match this lifecycle filter.')).toBeInTheDocument()
  expect(calls).toBe(2)
})

test('renders the Organization inbox in Simplified Chinese', async () => {
  const item = incidentItem(
    '00000000-0000-7000-8000-000000000035',
    'acknowledged',
    '结算 API',
  )
  item.health = { state: 'healthy', updatedAt: '2026-08-18T01:04:00Z' }
  mockInbox(() => ({ items: [item], nextCursor: null }))

  renderInbox('', 'zh-CN')

  expect(await screen.findByRole('heading', { name: '事件收件箱' })).toBeInTheDocument()
  expect(screen.getByRole('combobox', { name: '生命周期筛选' })).toHaveValue('active')
  const inbox = screen.getByRole('region', { name: '活动事件与近期事件' })
  expect(inbox.querySelector('.incident-state[data-state="acknowledged"]')).toHaveTextContent('已确认')
  expect(inbox.querySelector('.health-state[data-state="healthy"]')).toHaveTextContent('健康')
  expect(screen.getByText('没有当前或即将开始的维护窗口')).toBeInTheDocument()
  expect(screen.getByText('未记录因果运行')).toBeInTheDocument()
})
