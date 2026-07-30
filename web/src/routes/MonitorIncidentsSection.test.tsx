import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useParams } from 'react-router'
import { afterEach, expect, test, vi } from 'vitest'

import type { IncidentResponse } from '../api/incidents'
import { en } from '../i18n/en'
import { jsonResponse, renderRoutes } from '../test/renderWithProviders.tsx'
import MonitorIncidentsSection from './MonitorIncidentsSection.tsx'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const monitorId = '019f8f3d-5bb0-7cf5-b971-3c752c712310'
const incidentId = '019f8f3d-5bb0-7045-8bc0-ccb1d7765172'
const olderIncidentId = '019f8f3d-5bb0-7136-9ec1-acde91e22e04'
const openingRunId = '019f8f3d-5bb0-7de2-817b-71874054d7d1'
const recoveryRunId = '019f8f3d-5bb0-72d1-88db-d4f04113dadd'
const actorUserId = '019f8f3d-5bb0-766f-a272-acde6122f2d5'
const incidentsURL =
  `/api/v1/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}/incidents`
const monitorRoute =
  `/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}`

const incident: IncidentResponse = {
  id: incidentId,
  organizationId,
  projectId,
  monitorId,
  state: 'resolved',
  version: 3,
  openedTransitionId: '019f8f3d-5bb0-73b7-9b65-d738e14785c7',
  acknowledgedBy: actorUserId,
  acknowledgedAt: '2026-07-30T02:05:00Z',
  resolvedTransitionId: '019f8f3d-5bb0-744f-921d-25306399bb82',
  resolvedAt: '2026-07-30T02:10:00Z',
  createdAt: '2026-07-30T02:00:00Z',
  updatedAt: '2026-07-30T02:10:00Z',
  timeline: [
    {
      id: '019f8f3d-5bb0-7575-9fd5-28bf4b4fe0c4',
      incidentVersion: 1,
      kind: 'opened',
      healthTransitionId: '019f8f3d-5bb0-73b7-9b65-d738e14785c7',
      actorUserId: null,
      oldHealthState: 'degraded',
      newHealthState: 'down',
      policyVersion: 'phase1.v1',
      causalRunId: openingRunId,
      causalRunScheduledFor: '2026-07-30T02:00:00Z',
      counts: {
        configured: 1,
        eligible: 1,
        responding: 1,
        passing: 0,
        failing: 1,
        locationFault: 0,
        indeterminate: 0,
        missing: 0,
      },
      occurredAt: '2026-07-30T02:00:00Z',
    },
    {
      id: '019f8f3d-5bb0-76fb-91cb-3bc09e1d2759',
      incidentVersion: 2,
      kind: 'acknowledged',
      healthTransitionId: null,
      actorUserId,
      oldHealthState: null,
      newHealthState: null,
      policyVersion: null,
      causalRunId: null,
      causalRunScheduledFor: null,
      counts: null,
      occurredAt: '2026-07-30T02:05:00Z',
    },
    {
      id: '019f8f3d-5bb0-778b-b93a-76655306130c',
      incidentVersion: 3,
      kind: 'resolved',
      healthTransitionId: '019f8f3d-5bb0-744f-921d-25306399bb82',
      actorUserId: null,
      oldHealthState: 'degraded',
      newHealthState: 'healthy',
      policyVersion: 'phase1.v1',
      causalRunId: recoveryRunId,
      causalRunScheduledFor: '2026-07-30T02:10:00Z',
      counts: {
        configured: 1,
        eligible: 1,
        responding: 1,
        passing: 1,
        failing: 0,
        locationFault: 0,
        indeterminate: 0,
        missing: 0,
      },
      occurredAt: '2026-07-30T02:10:00Z',
    },
  ],
}

const olderIncident: IncidentResponse = {
  ...incident,
  id: olderIncidentId,
  state: 'open',
  version: 1,
  acknowledgedBy: null,
  acknowledgedAt: null,
  resolvedTransitionId: null,
  resolvedAt: null,
  createdAt: '2026-07-29T02:00:00Z',
  updatedAt: '2026-07-29T02:00:00Z',
  timeline: incident.timeline.slice(0, 1),
}

afterEach(() => {
  vi.restoreAllMocks()
})

function IncidentSectionRoute() {
  const { incidentId: selectedIncidentId } = useParams<'incidentId'>()
  return (
    <MonitorIncidentsSection
      organizationId={organizationId}
      projectId={projectId}
      monitorId={monitorId}
      incidentId={selectedIncidentId}
    />
  )
}

function renderSection(initialPath = monitorRoute) {
  return renderRoutes(
    [
      { path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId', element: <IncidentSectionRoute /> },
      { path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId/incidents/:incidentId', element: <IncidentSectionRoute /> },
      { path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId/runs/:runId', element: <p>Run evidence</p> },
    ],
    initialPath,
  )
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
}

test('lists Incidents and follows the opaque keyset cursor', async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    requested.push(url)
    const query = new URL(url, 'http://localhost').searchParams
    return Promise.resolve(
      jsonResponse(200, query.get('cursor') === null
        ? { items: [incident], nextCursor: 'opaque-incident-cursor' }
        : { items: [olderIncident], nextCursor: null }),
    )
  })
  renderSection()

  const section = await screen.findByRole('region', { name: en['incident.heading'] })
  expect(await within(section).findByRole('row', { name: /Resolved/ })).toBeInTheDocument()

  const user = userEvent.setup()
  await user.click(within(section).getByRole('button', { name: en['incident.loadMore'] }))

  expect(await within(section).findByRole('row', { name: /^Open / })).toBeInTheDocument()
  const firstQuery = new URL(requested[0] ?? '', 'http://localhost').searchParams
  expect(firstQuery.get('pageSize')).toBe('25')
  expect(requested.some((url) =>
    new URL(url, 'http://localhost').searchParams.get('cursor') === 'opaque-incident-cursor',
  )).toBe(true)
})

test('renders Incident lifecycle, immutable timeline, quorum, and causal Runs', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    if (url === `${incidentsURL}/${incidentId}`) {
      return Promise.resolve(jsonResponse(200, incident))
    }
    if (url.startsWith(`${incidentsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [incident], nextCursor: null }))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderSection(`${monitorRoute}/incidents/${incidentId}`)

  const section = await screen.findByRole('region', { name: en['incident.heading'] })
  const detailHeading = await within(section)
    .findByRole('heading', { name: en['incident.detail.heading'] })
  const detail = detailHeading.closest('section') as HTMLElement
  expect(detailHeading).toBeInTheDocument()
  expect(within(detail).getByText(en['incident.state.resolved'], { selector: '.incident-state' }))
    .toHaveAttribute('data-state', 'resolved')
  const openedKind = within(detail).getByText(en['incident.timeline.kind.opened'], {
    selector: '.incident-timeline-heading strong',
  })
  expect(openedKind).toBeInTheDocument()
  expect(within(detail).getByText(en['incident.timeline.kind.acknowledged'], {
    selector: '.incident-timeline-heading strong',
  })).toBeInTheDocument()
  expect(within(detail).getByText(en['incident.timeline.kind.resolved'], {
    selector: '.incident-timeline-heading strong',
  })).toBeInTheDocument()
  expect(within(detail).getByText('Degraded to Down')).toBeInTheDocument()
  expect(within(detail).getByText('Degraded to Healthy')).toBeInTheDocument()

  const openingEntry = openedKind.closest('li') as HTMLElement
  const failingCount = within(openingEntry)
    .getByText(en['health.counts.failing'])
    .parentElement
  expect(failingCount).toHaveTextContent('1')

  expect(within(detail).getByRole('link', { name: openingRunId }))
    .toHaveAttribute('href', `${monitorRoute}/runs/${openingRunId}`)
  expect(within(detail).getByRole('link', { name: recoveryRunId }))
    .toHaveAttribute('href', `${monitorRoute}/runs/${recoveryRunId}`)
  expect(within(detail).getByRole('link', { name: en['incident.detail.backToList'] }))
    .toHaveAttribute('href', monitorRoute)
  expect(within(detail).queryByRole('button', { name: en['incident.acknowledge.submit'] }))
    .not.toBeInTheDocument()
})

test('acknowledges an open Incident and refreshes its state and timeline', async () => {
  const acknowledgementEntry = {
    id: '019f8f3d-5bb0-77e0-a62c-e575926ad433',
    incidentVersion: 2,
    kind: 'acknowledged' as const,
    healthTransitionId: null,
    actorUserId,
    oldHealthState: null,
    newHealthState: null,
    policyVersion: null,
    causalRunId: null,
    causalRunScheduledFor: null,
    counts: null,
    occurredAt: '2026-07-30T02:05:00Z',
  }
  const acknowledgedIncident: IncidentResponse = {
    ...olderIncident,
    state: 'acknowledged',
    version: 2,
    acknowledgedBy: actorUserId,
    acknowledgedAt: acknowledgementEntry.occurredAt,
    updatedAt: acknowledgementEntry.occurredAt,
    timeline: [...olderIncident.timeline, acknowledgementEntry],
  }
  let currentIncident = olderIncident
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = requestURL(input)
    if (url === `${incidentsURL}/${olderIncidentId}/acknowledge` && init?.method === 'POST') {
      currentIncident = acknowledgedIncident
      return Promise.resolve(jsonResponse(200, acknowledgedIncident))
    }
    if (url === `${incidentsURL}/${olderIncidentId}`) {
      return Promise.resolve(jsonResponse(200, currentIncident))
    }
    if (url.startsWith(`${incidentsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [currentIncident], nextCursor: null }))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderSection(`${monitorRoute}/incidents/${olderIncidentId}`)

  const detailHeading = await screen.findByRole('heading', { name: en['incident.detail.heading'] })
  const detail = detailHeading.closest('section') as HTMLElement
  const user = userEvent.setup()
  const acknowledgeButton = await within(detail)
    .findByRole('button', { name: en['incident.acknowledge.submit'] })
  await user.click(acknowledgeButton)

  expect(await within(detail).findByText(en['incident.acknowledge.done'])).toBeInTheDocument()
  expect(within(detail).getByText(en['incident.state.acknowledged'], { selector: '.incident-state' }))
    .toHaveAttribute('data-state', 'acknowledged')
  expect(within(detail).getByText(en['incident.timeline.kind.acknowledged'], {
    selector: '.incident-timeline-heading strong',
  })).toBeInTheDocument()
  expect(within(detail).queryByRole('button', { name: en['incident.acknowledge.submit'] }))
    .not.toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    `${incidentsURL}/${olderIncidentId}/acknowledge`,
    expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
    }),
  )
})

test('localizes a rejected acknowledgement from its stable permission code', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = requestURL(input)
    if (url === `${incidentsURL}/${olderIncidentId}/acknowledge` && init?.method === 'POST') {
      return Promise.resolve(jsonResponse(403, {
        title: 'server wording that must not appear',
        status: 403,
        code: 'auth.forbidden',
      }))
    }
    if (url === `${incidentsURL}/${olderIncidentId}`) {
      return Promise.resolve(jsonResponse(200, olderIncident))
    }
    if (url.startsWith(`${incidentsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [olderIncident], nextCursor: null }))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderSection(`${monitorRoute}/incidents/${olderIncidentId}`)

  const detailHeading = await screen.findByRole('heading', { name: en['incident.detail.heading'] })
  const detail = detailHeading.closest('section') as HTMLElement
  const user = userEvent.setup()
  const acknowledgeButton = await within(detail)
    .findByRole('button', { name: en['incident.acknowledge.submit'] })
  await user.click(acknowledgeButton)

  expect(await within(detail).findByText(en['error.auth.forbidden'])).toBeInTheDocument()
  expect(within(detail).queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(within(detail).getByRole('button', { name: en['incident.acknowledge.submit'] }))
    .toBeEnabled()
})

test('localizes the code-less resolved Incident conflict contract', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = requestURL(input)
    if (url === `${incidentsURL}/${olderIncidentId}/acknowledge` && init?.method === 'POST') {
      return Promise.resolve(jsonResponse(409, {
        title: 'Incident acknowledgement rejected',
        status: 409,
        detail: 'A resolved Incident cannot be acknowledged.',
      }))
    }
    if (url === `${incidentsURL}/${olderIncidentId}`) {
      return Promise.resolve(jsonResponse(200, olderIncident))
    }
    if (url.startsWith(`${incidentsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [olderIncident], nextCursor: null }))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderSection(`${monitorRoute}/incidents/${olderIncidentId}`)

  const detailHeading = await screen.findByRole('heading', { name: en['incident.detail.heading'] })
  const detail = detailHeading.closest('section') as HTMLElement
  const user = userEvent.setup()
  await user.click(await within(detail)
    .findByRole('button', { name: en['incident.acknowledge.submit'] }))

  expect(await within(detail).findByText(en['incident.acknowledge.conflict']))
    .toBeInTheDocument()
  expect(within(detail).queryByText('A resolved Incident cannot be acknowledged.'))
    .not.toBeInTheDocument()
})
