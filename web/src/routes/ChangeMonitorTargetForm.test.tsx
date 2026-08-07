import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorResponse, MonitorRevisionResponse } from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import ChangeMonitorTargetForm from './ChangeMonitorTargetForm.tsx'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'

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

const revisionURL =
  '/api/v1/organizations/' + monitor.organizationId + '/projects/' + monitor.projectId +
  '/monitors/' + monitor.id + '/revisions'

const revision: MonitorRevisionResponse = {
  id: '019f8f3d-5bb0-7dc4-b682-0e2b75867d7a',
  monitorId: monitor.id,
  revisionNumber: 4,
  checkType: 'http',
  checkSchemaVersion: 1,
  checkConfiguration: { url: 'https://example.com/ready' },
  createdAt: '2026-07-30T03:00:00Z',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderForm(value = monitor) {
  return renderWithProviders(<ChangeMonitorTargetForm monitor={value} />, '', '/')
}

test('appends an HTTP revision and synchronizes detail and inventory query state', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(201, revision))
  const { queryClient } = renderForm()
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  queryClient.setQueryData(listKey, [monitor])
  queryClient.setQueryData(detailKey, monitor)

  const user = userEvent.setup()
  await user.type(
    screen.getByLabelText(en['monitor.targetEdit.url']),
    (revision.checkConfiguration as { url: string }).url,
  )
  await user.click(screen.getByRole('button', { name: en['monitor.targetEdit.submit'] }))

  expect(await screen.findByText(
    en['monitor.targetEdit.done'].replace('{revision}', '4'),
  )).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    revisionURL,
    expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
      body: JSON.stringify({
        checkSchemaVersion: 1,
        checkConfiguration: { url: 'https://example.com/ready' },
      }),
    }),
  )
  expect(queryClient.getQueryData(detailKey)).toEqual({
    ...monitor,
    latestRevisionNumber: 4,
    updatedAt: revision.createdAt,
  })
  expect(queryClient.getQueryState(listKey)?.isInvalidated).toBe(true)
})

test('localizes a rejected target from its stable field code', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(400, {
    title: 'server wording that must not appear',
    status: 400,
    errors: {
      'checkConfiguration.url': [{
        code: 'check.http.url.scheme',
        message: 'server wording that must not appear',
      }],
    },
  }))
  renderForm()

  const user = userEvent.setup()
  await user.type(screen.getByLabelText(en['monitor.targetEdit.url']), 'ftp://example.com')
  await user.click(screen.getByRole('button', { name: en['monitor.targetEdit.submit'] }))

  expect(await screen.findByText(en['error.check.http.url.scheme'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: en['monitor.targetEdit.submit'] })).toBeEnabled()
})

test.each([
  {
    name: 'concurrent updates',
    status: 409,
    code: 'monitor.concurrentUpdate',
    message: en['error.monitor.concurrentUpdate'],
  },
  {
    name: 'authorization failures',
    status: 403,
    code: 'auth.forbidden',
    message: en['error.auth.forbidden'],
  },
])('localizes $name from the shared Problem Details catalog', async ({
  status,
  code,
  message,
}) => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(status, {
    title: 'server wording that must not appear',
    status,
    code,
  }))
  renderForm()

  const user = userEvent.setup()
  await user.type(
    screen.getByLabelText(en['monitor.targetEdit.url']),
    'https://example.com/ready',
  )
  await user.click(screen.getByRole('button', { name: en['monitor.targetEdit.submit'] }))

  expect(await screen.findByText(message)).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: en['monitor.targetEdit.submit'] })).toBeEnabled()
})

test('does not offer a later revision form for archived or unconfigured Monitors', () => {
  const archived = renderForm({ ...monitor, state: 'archived' })

  expect(screen.queryByRole('form', { name: en['monitor.targetEdit.form'] }))
    .not.toBeInTheDocument()

  archived.unmount()
  renderForm({ ...monitor, state: 'draft', latestRevisionNumber: 0 })

  expect(screen.queryByRole('form', { name: en['monitor.targetEdit.form'] }))
    .not.toBeInTheDocument()
})
