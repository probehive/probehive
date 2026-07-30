import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorHealthResponse } from '../api/health'
import type { MonitorResponse } from '../api/monitors'
import type { ObservationResponse, RunResponse } from '../api/runs'
import { en } from '../i18n/en'
import { jsonResponse, renderRoutes } from '../test/renderWithProviders.tsx'
import MonitorPage from './MonitorPage.tsx'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const monitorId = '019f8f3d-5bb0-7cf5-b971-3c752c712310'
const passedRunId = '019f8f3d-5bb0-7de2-817b-71874054d7d1'
const skippedRunId = '019f8f3d-5bb0-7e18-a0b4-9c4b126487a0'
const expiredRunId = '019f8f3d-5bb0-7f37-88c8-67ce1958eac1'
const monitorURL =
  `/api/v1/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}`
const runsURL = `${monitorURL}/runs`
const healthURL = `${monitorURL}/health`
const monitorRoute =
  `/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}`

const monitor: MonitorResponse = {
  id: monitorId,
  organizationId,
  projectId,
  name: 'Homepage',
  checkType: 'http',
  state: 'active',
  intervalSeconds: 60,
  latestRevisionNumber: 3,
  createdAt: '2026-07-30T01:00:00Z',
  updatedAt: '2026-07-30T02:00:00Z',
}

const health: MonitorHealthResponse = {
  organizationId,
  projectId,
  monitorId,
  state: 'degraded',
  stableState: 'healthy',
  policyVersion: 'phase1.v1',
  version: 2,
  sourceRevisionNumber: 3,
  lastScheduledFor: '2026-07-30T02:00:00Z',
  lastDeterminateFinishedAt: '2026-07-30T02:00:00.001234Z',
  lastRunId: passedRunId,
  lastRunScheduledFor: '2026-07-30T02:00:00Z',
  candidate: {
    id: '019f8f3d-5bb0-7045-8bc0-ccb1d7765172',
    direction: 'failure',
    expectedEvidence: 'failing',
    sourceRevisionNumber: 3,
    triggeringRunId: passedRunId,
    triggeringScheduledFor: '2026-07-30T02:00:00Z',
    requestedAt: '2026-07-30T02:00:00.002Z',
  },
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
  transitionedAt: '2026-07-30T02:00:00.002Z',
  updatedAt: '2026-07-30T02:00:00.002Z',
}

const passedRun: RunResponse = {
  id: passedRunId,
  organizationId,
  projectId,
  monitorId,
  revisionNumber: 3,
  location: 'embedded',
  scheduledFor: '2026-07-30T02:00:00Z',
  kind: 'manual',
  outcome: 'passed',
  startedAt: '2026-07-30T02:00:00Z',
  finishedAt: '2026-07-30T02:00:00.001234Z',
  leaseExpiresAt: null,
  confirmation: null,
}

const skippedRun: RunResponse = {
  ...passedRun,
  id: skippedRunId,
  scheduledFor: '2026-07-30T01:59:00Z',
  kind: 'scheduled',
  outcome: 'skipped',
  startedAt: null,
  finishedAt: null,
}

const expiredRun: RunResponse = {
  ...passedRun,
  id: expiredRunId,
  scheduledFor: '2020-01-01T00:00:00Z',
  outcome: null,
  startedAt: '2020-01-01T00:00:00Z',
  finishedAt: null,
  leaseExpiresAt: '2020-01-01T00:01:00Z',
}

const observation: ObservationResponse = {
  runId: passedRunId,
  organizationId,
  scheduledFor: passedRun.scheduledFor,
  failureCode: '',
  failureClass: '',
  durationMicroseconds: 1234,
  phases: {
    connectMicroseconds: 350,
    tlsMicroseconds: 450,
    firstByteMicroseconds: 900,
  },
  http: {
    statusCode: 200,
    protocol: 'HTTP/2',
    redirectCount: 1,
    bodyBytes: 1024,
    bodyTruncated: true,
    tls: {
      version: 'TLS 1.3',
      cipherSuite: 'TLS_AES_128_GCM_SHA256',
      certificateExpiresAt: '2026-10-01T00:00:00Z',
    },
  },
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage(initialPath = monitorRoute) {
  return renderRoutes(
    [
      { path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId', element: <MonitorPage /> },
      { path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId/runs/:runId', element: <MonitorPage /> },
    ],
    initialPath,
  )
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
}

test('lists recent Runs and follows the opaque keyset cursor', async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    requested.push(url)
    if (url === monitorURL) {
      return Promise.resolve(jsonResponse(200, monitor))
    }
    if (url === healthURL) {
      return Promise.resolve(jsonResponse(200, health))
    }
    if (url.startsWith(`${runsURL}?`)) {
      const query = new URL(url, 'http://localhost').searchParams
      return Promise.resolve(
        jsonResponse(200, query.get('cursor') === null
          ? { items: [passedRun], nextCursor: 'opaque-cursor' }
          : { items: [skippedRun], nextCursor: null }),
      )
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage()

  expect(await screen.findByRole('heading', { name: 'Homepage' })).toBeInTheDocument()
  expect(await screen.findByRole('row', { name: /Passed/ })).toBeInTheDocument()

  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: en['run.loadMore'] }))

  expect(await screen.findByRole('row', { name: /Skipped/ })).toBeInTheDocument()
  const firstQuery = new URL(
    requested.find((url) => url.startsWith(`${runsURL}?`)) ?? '',
    'http://localhost',
  ).searchParams
  expect(firstQuery.get('notBefore')).toMatch(/T00:00:00\.000Z$/)
  expect(firstQuery.get('pageSize')).toBe('25')
  expect(requested.some((url) => new URL(url, 'http://localhost').searchParams.get('cursor') === 'opaque-cursor')).toBe(true)
})

test('loads one Run and its bounded HTTP Observation from the detail endpoints', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    if (url === monitorURL) {
      return Promise.resolve(jsonResponse(200, monitor))
    }
    if (url === healthURL) {
      return Promise.resolve(jsonResponse(200, health))
    }
    if (url.startsWith(`${runsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [passedRun], nextCursor: null }))
    }
    if (url === `${runsURL}/${passedRunId}`) {
      return Promise.resolve(jsonResponse(200, passedRun))
    }
    if (url === `${runsURL}/${passedRunId}/observation`) {
      return Promise.resolve(jsonResponse(200, observation))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage(`${monitorRoute}/runs/${passedRunId}`)

  expect(await screen.findByRole('heading', { name: en['run.detail.heading'] })).toBeInTheDocument()
  expect(await screen.findByRole('heading', { name: en['run.observation.heading'] })).toBeInTheDocument()
  expect(screen.getByText('1.234 ms')).toBeInTheDocument()
  expect(screen.getByText('HTTP/2')).toBeInTheDocument()
  expect(screen.getByText('1,024 bytes read (truncated)')).toBeInTheDocument()
  expect(screen.getByText('TLS_AES_128_GCM_SHA256')).toBeInTheDocument()
})

test('does not request an Observation for a skipped Run', async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    requested.push(url)
    if (url === monitorURL) {
      return Promise.resolve(jsonResponse(200, monitor))
    }
    if (url === healthURL) {
      return Promise.resolve(jsonResponse(200, health))
    }
    if (url.startsWith(`${runsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [skippedRun], nextCursor: null }))
    }
    if (url === `${runsURL}/${skippedRunId}`) {
      return Promise.resolve(jsonResponse(200, skippedRun))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage(`${monitorRoute}/runs/${skippedRunId}`)

  expect(await screen.findByText(en['run.observation.skipped'])).toBeInTheDocument()
  expect(requested).not.toContain(`${runsURL}/${skippedRunId}/observation`)
})

test('distinguishes an expired execution lease from an in-progress Run', async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    requested.push(url)
    if (url === monitorURL) {
      return Promise.resolve(jsonResponse(200, monitor))
    }
    if (url === healthURL) {
      return Promise.resolve(jsonResponse(200, health))
    }
    if (url.startsWith(`${runsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [expiredRun], nextCursor: null }))
    }
    if (url === `${runsURL}/${expiredRunId}`) {
      return Promise.resolve(jsonResponse(200, expiredRun))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage(`${monitorRoute}/runs/${expiredRunId}`)

  expect(await screen.findByText(en['run.observation.leaseExpired'])).toBeInTheDocument()
  expect(screen.getAllByText(en['run.outcome.leaseExpired'])).toHaveLength(2)
  expect(requested).not.toContain(`${runsURL}/${expiredRunId}/observation`)
})

test('renders evaluated health, quorum counts, and confirmation causality', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    if (url === monitorURL) {
      return Promise.resolve(jsonResponse(200, monitor))
    }
    if (url === healthURL) {
      return Promise.resolve(jsonResponse(200, health))
    }
    if (url.startsWith(`${runsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [passedRun], nextCursor: null }))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage()

  const section = await screen.findByRole('region', { name: en['health.heading'] })
  await within(section).findByText(en['health.state.degraded'])
  expect(within(section).getByText(en['health.state.degraded'])).toHaveAttribute(
    'data-state',
    'degraded',
  )
  expect(within(section).getByText(en['health.state.healthy'])).toBeInTheDocument()
  expect(within(section).getByText('phase1.v1')).toBeInTheDocument()
  expect(within(section).getByText(en['health.direction.failure'])).toBeInTheDocument()

  const expectedEvidence = within(section).getByText(en['health.candidate.expected']).parentElement
  expect(expectedEvidence).toHaveTextContent(en['health.evidence.failing'])
  const countsList = section.querySelector('.health-counts') as HTMLElement
  const counts = within(countsList).getByText(en['health.counts.failing']).parentElement
  expect(counts).not.toBeNull()
  expect(counts).toHaveTextContent('1')

  const causalRunLinks = within(section).getAllByRole('link', { name: passedRunId })
  expect(causalRunLinks).toHaveLength(2)
  for (const link of causalRunLinks) {
    expect(link).toHaveAttribute('href', `${monitorRoute}/runs/${passedRunId}`)
  }
})

test('treats a missing health projection as not evaluated instead of Unknown', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
    const url = requestURL(input)
    if (url === monitorURL) {
      return Promise.resolve(jsonResponse(200, monitor))
    }
    if (url === healthURL) {
      return Promise.resolve(jsonResponse(404, { status: 404 }))
    }
    if (url.startsWith(`${runsURL}?`)) {
      return Promise.resolve(jsonResponse(200, { items: [], nextCursor: null }))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPage()

  const section = await screen.findByRole('region', { name: en['health.heading'] })
  await within(section).findByText(en['health.notAvailable'])
  expect(within(section).getByText(en['health.notAvailable'])).toBeInTheDocument()
  expect(within(section).queryByText(en['health.state.unknown'])).not.toBeInTheDocument()
})
