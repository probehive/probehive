import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { AlertDeliveryResponse, AlertResponse } from '../api/alerts'
import { en } from '../i18n/en'
import { zhCN } from '../i18n/zh-CN'
import { jsonResponse, renderRoutes } from '../test/renderWithProviders.tsx'
import MonitorAlertsSection from './MonitorAlertsSection.tsx'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const monitorId = '019f8f3d-5bb0-7cf5-b971-3c752c712310'
const incidentId = '019f8f3d-5bb0-7045-8bc0-ccb1d7765172'
const olderIncidentId = '019f8f3d-5bb0-7136-9ec1-acde91e22e04'
const alertsURL =
  `/api/v1/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}/alerts`
const monitorRoute =
  `/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}`

const resolvedAlert: AlertResponse = {
  id: '019f8f3d-5bb0-7575-9fd5-28bf4b4fe0c4',
  organizationId,
  projectId,
  monitorId,
  incidentId,
  incidentVersion: 3,
  kind: 'incident.resolved',
  occurredAt: '2026-07-30T02:10:00Z',
  createdAt: '2026-07-30T02:10:00.005Z',
}

const deliveriesURL = `${alertsURL}/${resolvedAlert.id}/deliveries`

const delivery: AlertDeliveryResponse = {
  id: '019f8f3d-5bb0-7990-8b16-7b9485f79d3a',
  channel: 'webhook',
  integrationId: '019f8f3d-5bb0-7a01-8f1a-c2f2cf2d1f30',
  integrationVersion: 4,
  secretVersion: 2,
  routedAt: '2026-07-30T02:10:00.010Z',
  suppressionReason: null,
  maintenanceWindowId: null,
  attempts: [
    {
      sequence: 1,
      startedAt: '2026-07-30T02:10:01Z',
      finishedAt: '2026-07-30T02:10:02Z',
      outcome: 'failed',
      httpStatus: 503,
      failureCode: 'webhook.delivery.http.retryable',
    },
    {
      sequence: 2,
      startedAt: '2026-07-30T02:10:09Z',
      finishedAt: '2026-07-30T02:10:10Z',
      outcome: 'succeeded',
      httpStatus: 204,
      failureCode: null,
    },
  ],
}

const suppressedDelivery: AlertDeliveryResponse = {
  ...delivery,
  id: '019f8f3d-5bb0-7990-8b16-7b9485f79d3b',
  suppressionReason: 'maintenance',
  maintenanceWindowId: '019f8f3d-5bb0-7990-8b16-7b9485f79d3c',
  attempts: [],
}

const openedAlert: AlertResponse = {
  ...resolvedAlert,
  id: '019f8f3d-5bb0-778b-b93a-76655306130c',
  incidentId: olderIncidentId,
  incidentVersion: 1,
  kind: 'incident.opened',
  occurredAt: '2026-07-29T02:00:00Z',
  createdAt: '2026-07-29T02:00:00.005Z',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderSection(locale = 'en') {
  return renderRoutes(
    [
      {
        path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId',
        element: (
          <MonitorAlertsSection
            organizationId={organizationId}
            projectId={projectId}
            monitorId={monitorId}
          />
        ),
      },
      {
        path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId/incidents/:incidentId',
        element: <p>Incident evidence</p>,
      },
    ],
    monitorRoute,
    { locale },
  )
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
}

test('lists Alert intents, links their source Incidents, and follows the opaque cursor', async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    requested.push(url)
    const query = new URL(url, 'http://localhost').searchParams
    return Promise.resolve(
      jsonResponse(200, query.get('cursor') === null
        ? { items: [resolvedAlert], nextCursor: 'opaque-alert-cursor' }
        : { items: [openedAlert], nextCursor: null }),
    )
  })
  renderSection()

  const section = await screen.findByRole('region', { name: en['alert.heading'] })
  expect(within(section).getByText(en['alert.scope'])).toBeInTheDocument()
  const resolvedRow = await within(section).findByRole('row', { name: /Incident resolved/ })
  expect(within(resolvedRow).getByRole('link', { name: en['alert.view'] }))
    .toHaveAttribute('href', `${monitorRoute}/incidents/${incidentId}`)

  const user = userEvent.setup()
  await user.click(within(section).getByRole('button', { name: en['alert.loadMore'] }))

  const openedRow = await within(section).findByRole('row', { name: /Incident opened/ })
  expect(within(openedRow).getByRole('link', { name: en['alert.view'] }))
    .toHaveAttribute('href', `${monitorRoute}/incidents/${olderIncidentId}`)
  const firstQuery = new URL(requested[0] ?? '', 'http://localhost').searchParams
  expect(firstQuery.get('pageSize')).toBe('25')
  expect(requested.every((url) => url.startsWith(`${alertsURL}?`))).toBe(true)
  expect(requested.some((url) =>
    new URL(url, 'http://localhost').searchParams.get('cursor') === 'opaque-alert-cursor',
  )).toBe(true)
})

test('loads scoped delivery evidence on demand without rendering sensitive material', async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    requested.push(url)
    if (url === deliveriesURL) {
      return Promise.resolve(jsonResponse(200, {
        items: [
          {
            ...delivery,
            destinationUrl: 'https://hooks.example.test/private',
            signingSecret: 'phwh_should-not-render',
            providerText: 'provider response should not render',
          },
          suppressedDelivery,
        ],
      }))
    }
    return Promise.resolve(jsonResponse(200, { items: [resolvedAlert], nextCursor: null }))
  })
  renderSection()

  const section = await screen.findByRole('region', { name: en['alert.heading'] })
  const row = await within(section).findByRole('row', { name: /Incident resolved/ })
  expect(requested).not.toContain(deliveriesURL)
  const user = userEvent.setup()
  await user.click(within(row).getByRole('button', { name: en['alert.delivery.show'] }))

  const evidenceHeading = await within(section).findByRole(
    'heading', { name: en['alert.delivery.heading'] },
  )
  const evidence = evidenceHeading.parentElement as HTMLElement
  expect(within(evidence).getByText('503')).toBeInTheDocument()
  expect(within(evidence).getByText('webhook.delivery.http.retryable')).toBeInTheDocument()
  expect(within(evidence).getByText(en['alert.delivery.outcome.succeeded'])).toBeInTheDocument()
  expect(within(evidence).getByText(en['alert.delivery.suppressed.maintenance'])).toBeInTheDocument()
  expect(within(evidence).getByText(
    suppressedDelivery.maintenanceWindowId as string,
  )).toBeInTheDocument()
  expect(requested).toContain(deliveriesURL)
  expect(screen.queryByText('hooks.example.test')).not.toBeInTheDocument()
  expect(screen.queryByText('phwh_should-not-render')).not.toBeInTheDocument()
  expect(screen.queryByText('provider response should not render')).not.toBeInTheDocument()

  await user.click(within(row).getByRole('button', { name: en['alert.delivery.hide'] }))
  expect(within(section).queryByRole('heading', { name: en['alert.delivery.heading'] }))
    .not.toBeInTheDocument()
})

test('localizes an empty Alert intent history in Simplified Chinese', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    jsonResponse(200, { items: [], nextCursor: null }),
  )
  renderSection('zh-CN')

  const section = await screen.findByRole('region', { name: zhCN['alert.heading'] })
  expect(within(section).getByText(zhCN['alert.scope'])).toBeInTheDocument()
  expect(await within(section).findByText(zhCN['alert.empty'])).toBeInTheDocument()
})

test('localizes empty delivery evidence in Simplified Chinese', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    if (url === deliveriesURL) {
      return Promise.resolve(jsonResponse(200, { items: [] }))
    }
    return Promise.resolve(jsonResponse(200, { items: [resolvedAlert], nextCursor: null }))
  })
  renderSection('zh-CN')

  const section = await screen.findByRole('region', { name: zhCN['alert.heading'] })
  const row = await within(section).findByRole('row', { name: /事件解决/ })
  const user = userEvent.setup()
  await user.click(within(row).getByRole('button', { name: zhCN['alert.delivery.show'] }))
  expect(await within(section).findByText(zhCN['alert.delivery.empty'])).toBeInTheDocument()
})
