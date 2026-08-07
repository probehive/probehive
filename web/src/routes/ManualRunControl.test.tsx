import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorResponse } from '../api/monitors'
import type { RunResponse } from '../api/runs'
import { en } from '../i18n/en'
import { zhCN } from '../i18n/zh-CN'
import { jsonResponse, renderRoutes } from '../test/renderWithProviders.tsx'
import ManualRunControl from './ManualRunControl.tsx'

const monitor: MonitorResponse = {
  id: '019f8f3d-5bb0-7cf5-b971-3c752c712310',
  organizationId: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  projectId: '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420',
  name: 'Homepage',
  checkType: 'http',
  state: 'active',
  intervalSeconds: 60,
  latestRevisionNumber: 3,
  createdAt: '2026-07-30T01:00:00Z',
  updatedAt: '2026-07-30T02:00:00Z',
}

const run: RunResponse = {
  id: '019f8f3d-5bb0-7de2-817b-71874054d7d1',
  organizationId: monitor.organizationId,
  projectId: monitor.projectId,
  monitorId: monitor.id,
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

const monitorRoute =
  '/organizations/' + monitor.organizationId + '/projects/' + monitor.projectId +
  '/monitors/' + monitor.id
const runsURL =
  '/api/v1/organizations/' + monitor.organizationId + '/projects/' + monitor.projectId +
  '/monitors/' + monitor.id + '/runs'

afterEach(() => {
  vi.restoreAllMocks()
})

function renderControl(value = monitor, locale = 'en') {
  return renderRoutes(
    [
      {
        path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId',
        element: <ManualRunControl monitor={value} />,
      },
      {
        path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId/runs/:runId',
        element: <p>Run destination</p>,
      },
    ],
    monitorRoute,
    { locale },
  )
}

test('posts without a payload, invalidates Run evidence, and opens the returned Run', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(201, run))
  const { queryClient } = renderControl()
  const listKey = [
    'runs',
    monitor.organizationId,
    monitor.projectId,
    monitor.id,
    '2026-07-01T00:00:00.000Z',
  ]
  queryClient.setQueryData(listKey, { items: [], nextCursor: null })

  await userEvent.setup().click(screen.getByRole('button', { name: en['run.manual.action'] }))

  expect(await screen.findByText('Run destination')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    runsURL,
    expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
      body: undefined,
    }),
  )
  expect(queryClient.getQueryState(listKey)?.isInvalidated).toBe(true)
})

test('disables the action and shows progress while execution is pending', async () => {
  vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise<Response>(() => {}))
  renderControl()

  await userEvent.setup().click(screen.getByRole('button', { name: en['run.manual.action'] }))

  expect(screen.getByRole('button', { name: en['run.manual.running'] })).toBeDisabled()
})

test('localizes an unconfigured Monitor failure and remains available for retry', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
    .mockResolvedValueOnce(jsonResponse(409, {
      title: 'server wording that must not appear',
      status: 409,
      code: 'run.manual.unconfigured',
    }))
    .mockResolvedValueOnce(jsonResponse(201, run))
  renderControl({ ...monitor, state: 'draft', latestRevisionNumber: 0 })
  const user = userEvent.setup()

  await user.click(screen.getByRole('button', { name: en['run.manual.action'] }))

  expect(await screen.findByText(en['error.run.manual.unconfigured'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  const retry = screen.getByRole('button', { name: en['run.manual.action'] })
  expect(retry).toBeEnabled()

  await user.click(retry)
  expect(await screen.findByText('Run destination')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledTimes(2)
})

test('localizes worker unavailability in Simplified Chinese', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(503, {
    title: 'server wording that must not appear',
    status: 503,
    code: 'run.manual.unavailable',
  }))
  renderControl(monitor, 'zh-CN')

  await userEvent.setup().click(screen.getByRole('button', {
    name: zhCN['run.manual.action'],
  }))

  expect(await screen.findByText(
    zhCN['error.run.manual.unavailable'],
  )).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})

test('does not expose execution on an archived Monitor', () => {
  renderControl({ ...monitor, state: 'archived' })

  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
