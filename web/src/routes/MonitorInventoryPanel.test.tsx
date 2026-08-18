import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { setAntiforgeryForTests } from '../api/http'
import type {
  MonitorInventoryItemResponse,
  MonitorInventoryPageResponse,
  MonitorResponse,
} from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import MonitorInventoryPanel from './MonitorInventoryPanel.tsx'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const monitorId = '019f8f3d-5bb0-7cf5-b971-3c752c712310'
const inventoryURL = '/api/v1/organizations/' + organizationId
  + '/projects/' + projectId + '/monitor-inventory'

const active: MonitorResponse = {
  id: monitorId,
  organizationId,
  projectId,
  name: 'API Gateway',
  checkType: 'http',
  state: 'active',
  intervalSeconds: 60,
  latestRevisionNumber: 1,
  createdAt: '2026-08-18T01:00:00+00:00',
  updatedAt: '2026-08-18T01:05:00+00:00',
}

function inventoryItem(monitor: MonitorResponse = active): MonitorInventoryItemResponse {
  return {
    monitor,
    health: { state: 'down', updatedAt: '2026-08-18T01:04:00+00:00' },
    lastRun: {
      id: '019f8f3d-5bb0-7cf5-b971-3c752c712311',
      outcome: 'failed',
      scheduledFor: '2026-08-18T01:03:00+00:00',
    },
    maintenance: {
      state: 'active',
      windowId: '019f8f3d-5bb0-7cf5-b971-3c752c712312',
      startsAt: '2026-08-18T01:02:00+00:00',
      endsAt: '2026-08-18T02:02:00+00:00',
    },
  }
}

function page(
  items: MonitorInventoryItemResponse[],
  currentPage = 1,
  total = items.length,
): MonitorInventoryPageResponse {
  return { items, page: currentPage, pageSize: 10, total }
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
}

function renderPanel(initialPath = '/') {
  return renderWithProviders(
    <MonitorInventoryPanel organizationId={organizationId} projectId={projectId} />,
    '',
    initialPath,
  )
}

afterEach(() => {
  setAntiforgeryForTests(null)
  vi.restoreAllMocks()
})

test('loads combined filters from the URL without conflating operational dimensions', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, page([inventoryItem()])))
  renderPanel('/?monitorSearch=api&monitorState=active&monitorHealth=down&monitorRun=failed'
    + '&monitorMaintenance=active&monitorSort=updatedAt&monitorDirection=desc')

  expect(await screen.findByRole('link', { name: /API Gateway/ })).toBeInTheDocument()
  expect(screen.getAllByText(en['health.state.down']).length).toBeGreaterThan(0)
  expect(screen.getAllByText(en['monitor.inventory.run.failed']).length).toBeGreaterThan(0)
  expect(screen.getAllByText(en['maintenance.status.active']).length).toBeGreaterThan(0)
  expect(fetchMock).toHaveBeenCalledWith(
    inventoryURL + '?search=api&state=active&health=down&runOutcome=failed'
      + '&maintenance=active&sort=updatedAt&direction=desc&page=1&pageSize=10',
  )
})

test('navigates between inventory pages', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    if (url.includes('page=2')) {
      return Promise.resolve(jsonResponse(200, page([
        inventoryItem({ ...active, id: monitorId.slice(0, -1) + '9', name: 'Worker API' }),
      ], 2, 11)))
    }
    return Promise.resolve(jsonResponse(200, page([inventoryItem()], 1, 11)))
  })
  renderPanel()

  expect(await screen.findByRole('link', { name: /API Gateway/ })).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: en['monitor.inventory.next'] }))

  expect(await screen.findByRole('link', { name: /Worker API/ })).toBeInTheDocument()
  expect(screen.getByText('Page 2 of 2')).toBeInTheDocument()
})

test('recovers from an empty filtered result', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    return Promise.resolve(jsonResponse(
      200,
      url.includes('state=archived') ? page([]) : page([inventoryItem()]),
    ))
  })
  renderPanel('/?monitorState=archived')

  expect(await screen.findByText(en['monitor.inventory.noResults'])).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: en['monitor.inventory.clear'] }))

  expect(await screen.findByRole('link', { name: /API Gateway/ })).toBeInTheDocument()
  expect(screen.getByLabelText(en['monitor.state'])).toHaveValue('')
})

test('pauses a Monitor from the inventory and refreshes the row', async () => {
  setAntiforgeryForTests({
    headerName: 'X-ProbeHive-Antiforgery',
    requestToken: 'token',
  })
  let current = active
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = requestURL(input)
    if (url === '/api/v1/organizations/' + organizationId + '/projects/' + projectId
      + '/monitors/' + monitorId + '/state' && init?.method === 'PUT') {
      current = { ...active, state: 'paused', updatedAt: '2026-08-18T01:06:00+00:00' }
      return Promise.resolve(jsonResponse(200, current))
    }
    if (url.startsWith(inventoryURL)) {
      return Promise.resolve(jsonResponse(200, page([inventoryItem(current)])))
    }
    return Promise.reject(new Error('Unexpected fetch: ' + url))
  })
  renderPanel()

  await userEvent.setup().click(await screen.findByRole('button', { name: en['monitor.lifecycle.pause'] }))

  expect(await screen.findByRole('button', { name: en['monitor.lifecycle.activate'] })).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
    '/api/v1/organizations/' + organizationId + '/projects/' + projectId
      + '/monitors/' + monitorId + '/state',
    expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ state: 'paused' }),
    }),
  ))
})
