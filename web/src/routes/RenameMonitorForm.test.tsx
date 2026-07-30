import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorResponse } from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import { monitorQueryKey, monitorsQueryKey } from './monitorQueries'
import RenameMonitorForm from './RenameMonitorForm.tsx'

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

const nameURL =
  `/api/v1/organizations/${monitor.organizationId}/projects/${monitor.projectId}` +
  `/monitors/${monitor.id}/name`

afterEach(() => {
  vi.restoreAllMocks()
})

function renderForm(value = monitor) {
  return renderWithProviders(<RenameMonitorForm monitor={value} />, '', '/')
}

test('renames a Monitor and updates the detail and inventory query state', async () => {
  const renamed = {
    ...monitor,
    name: 'Edge API',
    updatedAt: '2026-07-30T03:00:00Z',
  }
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, renamed))
  const { queryClient } = renderForm()
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  queryClient.setQueryData(listKey, [monitor])
  queryClient.setQueryData(detailKey, monitor)

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.rename.name'])
  expect(field).toHaveValue(monitor.name)
  await user.clear(field)
  await user.type(field, renamed.name)
  await user.click(screen.getByRole('button', { name: en['monitor.rename.submit'] }))

  expect(await screen.findByText(en['monitor.rename.done'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    nameURL,
    expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
      body: JSON.stringify({ name: renamed.name }),
    }),
  )
  expect(queryClient.getQueryData(detailKey)).toEqual(renamed)
  expect(queryClient.getQueryState(listKey)?.isInvalidated).toBe(true)
})

test('localizes a rejected name from its stable field code', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(400, {
    title: 'server wording that must not appear',
    status: 400,
    errors: {
      name: [{ code: 'monitor.name.invalid', message: 'server wording that must not appear' }],
    },
  }))
  renderForm()

  const user = userEvent.setup()
  await user.clear(screen.getByLabelText(en['monitor.rename.name']))
  await user.click(screen.getByRole('button', { name: en['monitor.rename.submit'] }))

  expect(await screen.findByText(en['error.monitor.name.invalid'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})

test('localizes a concurrent rename and leaves the form available for retry', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(409, {
    title: 'server wording that must not appear',
    status: 409,
    code: 'monitor.concurrentUpdate',
  }))
  renderForm()

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.rename.name'])
  await user.clear(field)
  await user.type(field, 'Edge API')
  await user.click(screen.getByRole('button', { name: en['monitor.rename.submit'] }))

  expect(await screen.findByText(en['error.monitor.concurrentUpdate'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: en['monitor.rename.submit'] })).toBeEnabled()
})

test('localizes authorization failures from the shared Problem Details catalog', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(403, {
    title: 'server wording that must not appear',
    status: 403,
    code: 'auth.forbidden',
  }))
  renderForm()

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.rename.name'])
  await user.clear(field)
  await user.type(field, 'Edge API')
  await user.click(screen.getByRole('button', { name: en['monitor.rename.submit'] }))

  expect(await screen.findByText(en['error.auth.forbidden'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})

test('does not offer rename controls for an archived Monitor', () => {
  renderForm({ ...monitor, state: 'archived' })

  expect(screen.queryByRole('form', { name: en['monitor.rename.form'] })).not.toBeInTheDocument()
})
