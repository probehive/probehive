import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { setAntiforgeryForTests } from '../api/http'
import type { MonitorResponse } from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import MonitorsPanel from './MonitorsPanel.tsx'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const monitorId = '019f8f3d-5bb0-7cf5-b971-3c752c712310'
const collectionURL = `/api/v1/organizations/${organizationId}/projects/${projectId}/monitors`

const draft: MonitorResponse = {
  id: monitorId,
  organizationId,
  projectId,
  name: 'Homepage',
  checkType: 'http',
  state: 'draft',
  intervalSeconds: 60,
  latestRevisionNumber: 0,
  createdAt: '2026-07-30T01:00:00+00:00',
  updatedAt: '2026-07-30T01:00:00+00:00',
}
const active: MonitorResponse = {
  ...draft,
  state: 'active',
  latestRevisionNumber: 1,
  updatedAt: '2026-07-30T01:01:00+00:00',
}

afterEach(() => {
  setAntiforgeryForTests(null)
  vi.restoreAllMocks()
})

function renderPanel() {
  return renderWithProviders(
    <MonitorsPanel organizationId={organizationId} projectId={projectId} />,
    '',
    '/',
  )
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
}

test('lists existing Monitors with their operational state', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, [active]))
  renderPanel()

  expect(await screen.findByRole('row', { name: /Homepage Active/ })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /Homepage/ })).toHaveAttribute(
    'href',
    `/organizations/${organizationId}/projects/${projectId}/monitors/${monitorId}`,
  )
  expect(screen.getByText('60 seconds')).toBeInTheDocument()
  expect(screen.getByText('v1')).toBeInTheDocument()
})

test('creates, configures, and activates an HTTP Monitor', async () => {
  let isActive = false
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = requestURL(input)
    if (url === '/api/v1/auth/antiforgery') {
      return Promise.resolve(jsonResponse(200, {
        headerName: 'X-ProbeHive-Antiforgery', requestToken: 'token',
      }))
    }
    if (url === collectionURL && init?.method === 'POST') {
      return Promise.resolve(jsonResponse(201, draft))
    }
    if (url === `${collectionURL}/${monitorId}/revisions` && init?.method === 'POST') {
      return Promise.resolve(jsonResponse(201, {
        id: '019f8f3d-5bb0-7dc4-b682-0e2b75867d7a',
        monitorId,
        revisionNumber: 1,
        checkType: 'http',
        checkSchemaVersion: 1,
        checkConfiguration: { url: 'https://example.com/health' },
        createdAt: '2026-07-30T01:00:30+00:00',
      }))
    }
    if (url === `${collectionURL}/${monitorId}/state` && init?.method === 'PUT') {
      isActive = true
      return Promise.resolve(jsonResponse(200, active))
    }
    if (url === collectionURL && init?.method === undefined) {
      return Promise.resolve(jsonResponse(200, isActive ? [active] : []))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  renderPanel()

  expect(await screen.findByText(en['monitor.empty'])).toBeInTheDocument()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText(en['monitor.setup.name']), 'Homepage')
  await user.type(screen.getByLabelText(en['monitor.setup.url']), 'https://example.com/health')
  await user.click(screen.getByRole('button', { name: en['monitor.setup.create'] }))

  expect(await screen.findByText('Homepage is active.')).toBeInTheDocument()
  expect(await screen.findByRole('row', { name: /Homepage Active/ })).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    collectionURL,
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ name: 'Homepage', checkType: 'http', intervalSeconds: 60 }),
    }),
  )
  expect(fetchMock).toHaveBeenCalledWith(
    `${collectionURL}/${monitorId}/revisions`,
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        checkSchemaVersion: 1,
        checkConfiguration: { url: 'https://example.com/health' },
      }),
    }),
  )
  expect(fetchMock).toHaveBeenCalledWith(
    `${collectionURL}/${monitorId}/state`,
    expect.objectContaining({ method: 'PUT', body: JSON.stringify({ state: 'active' }) }),
  )
})

test('continues the same Draft after revision validation fails', async () => {
  let createAttempts = 0
  let revisionAttempts = 0
  let current: MonitorResponse | null = null
  vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = requestURL(input)
    if (url === '/api/v1/auth/antiforgery') {
      return Promise.resolve(jsonResponse(200, {
        headerName: 'X-ProbeHive-Antiforgery', requestToken: 'token',
      }))
    }
    if (url === collectionURL && init?.method === 'POST') {
      createAttempts += 1
      current = draft
      return Promise.resolve(jsonResponse(201, draft))
    }
    if (url === `${collectionURL}/${monitorId}/revisions` && init?.method === 'POST') {
      revisionAttempts += 1
      if (revisionAttempts === 1) {
        return Promise.resolve(jsonResponse(400, {
          title: 'One or more validation errors occurred.',
          status: 400,
          errors: {
            'checkConfiguration.url': [{
              code: 'check.http.url.scheme',
              message: 'server wording that must not appear',
            }],
          },
        }))
      }
      current = { ...draft, latestRevisionNumber: 1 }
      return Promise.resolve(jsonResponse(201, {
        id: '019f8f3d-5bb0-7dc4-b682-0e2b75867d7a',
        monitorId,
        revisionNumber: 1,
        checkType: 'http',
        checkSchemaVersion: 1,
        checkConfiguration: { url: 'https://example.com/health' },
        createdAt: '2026-07-30T01:00:30+00:00',
      }))
    }
    if (url === `${collectionURL}/${monitorId}/state` && init?.method === 'PUT') {
      current = active
      return Promise.resolve(jsonResponse(200, active))
    }
    if (url === collectionURL && init?.method === undefined) {
      return Promise.resolve(jsonResponse(200, current === null ? [] : [current]))
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
  const view = renderPanel()

  await screen.findByText(en['monitor.empty'])
  const user = userEvent.setup()
  await user.type(screen.getByLabelText(en['monitor.setup.name']), 'Homepage')
  const urlField = screen.getByLabelText(en['monitor.setup.url'])
  await user.type(urlField, 'ftp://example.com/health')
  await user.click(screen.getByRole('button', { name: en['monitor.setup.create'] }))

  expect(await screen.findByText(en['error.check.http.url.scheme'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(screen.getByLabelText(en['monitor.setup.name'])).toBeDisabled()

  view.unmount()
  renderPanel()
  const resumeButton = await screen.findByRole('button', { name: en['monitor.setup.resume'] })
  await user.click(resumeButton)
  expect(screen.getByLabelText(en['monitor.setup.name'])).toHaveValue('Homepage')
  const resumedURLField = screen.getByLabelText(en['monitor.setup.url'])
  await user.type(resumedURLField, 'https://example.com/health')
  await user.click(screen.getByRole('button', { name: en['monitor.setup.configure'] }))

  expect(await screen.findByText('Homepage is active.')).toBeInTheDocument()
  expect(createAttempts).toBe(1)
  expect(revisionAttempts).toBe(2)
})
