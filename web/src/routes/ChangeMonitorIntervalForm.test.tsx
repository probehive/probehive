import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorResponse } from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import ChangeMonitorIntervalForm from './ChangeMonitorIntervalForm.tsx'
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

const intervalURL =
  `/api/v1/organizations/${monitor.organizationId}/projects/${monitor.projectId}` +
  `/monitors/${monitor.id}/interval`

afterEach(() => {
  vi.restoreAllMocks()
})

function renderForm(value = monitor) {
  return renderWithProviders(<ChangeMonitorIntervalForm monitor={value} />, '', '/')
}

test('changes a Monitor interval and updates the detail and inventory query state', async () => {
  const updated = {
    ...monitor,
    intervalSeconds: 300,
    updatedAt: '2026-07-30T03:00:00Z',
  }
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, updated))
  const { queryClient } = renderForm()
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  queryClient.setQueryData(listKey, [monitor])
  queryClient.setQueryData(detailKey, monitor)

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.intervalEdit.seconds'])
  expect(field).toHaveValue(monitor.intervalSeconds)
  await user.clear(field)
  await user.type(field, String(updated.intervalSeconds))
  await user.click(screen.getByRole('button', { name: en['monitor.intervalEdit.submit'] }))

  expect(await screen.findByText(en['monitor.intervalEdit.done'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    intervalURL,
    expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
      body: JSON.stringify({ intervalSeconds: updated.intervalSeconds }),
    }),
  )
  expect(queryClient.getQueryData(detailKey)).toEqual(updated)
  expect(queryClient.getQueryState(listKey)?.isInvalidated).toBe(true)
  expect(updated.latestRevisionNumber).toBe(monitor.latestRevisionNumber)
})

test('localizes a rejected interval from its stable field code', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(400, {
    title: 'server wording that must not appear',
    status: 400,
    errors: {
      intervalSeconds: [{
        code: 'monitor.interval.invalid',
        message: 'server wording that must not appear',
      }],
    },
  }))
  renderForm()

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.intervalEdit.seconds'])
  await user.clear(field)
  await user.type(field, '30')
  await user.click(screen.getByRole('button', { name: en['monitor.intervalEdit.submit'] }))

  expect(await screen.findByText(en['error.monitor.interval.invalid'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})

test('localizes a concurrent update and leaves the form available for retry', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(409, {
    title: 'server wording that must not appear',
    status: 409,
    code: 'monitor.concurrentUpdate',
  }))
  renderForm()

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.intervalEdit.seconds'])
  await user.clear(field)
  await user.type(field, '300')
  await user.click(screen.getByRole('button', { name: en['monitor.intervalEdit.submit'] }))

  expect(await screen.findByText(en['error.monitor.concurrentUpdate'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: en['monitor.intervalEdit.submit'] })).toBeEnabled()
})

test('localizes authorization failures from the shared Problem Details catalog', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(403, {
    title: 'server wording that must not appear',
    status: 403,
    code: 'auth.forbidden',
  }))
  renderForm()

  const user = userEvent.setup()
  const field = screen.getByLabelText(en['monitor.intervalEdit.seconds'])
  await user.clear(field)
  await user.type(field, '300')
  await user.click(screen.getByRole('button', { name: en['monitor.intervalEdit.submit'] }))

  expect(await screen.findByText(en['error.auth.forbidden'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})

test('does not submit an empty interval through a direct form submission', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
  renderForm()

  const user = userEvent.setup()
  await user.clear(screen.getByLabelText(en['monitor.intervalEdit.seconds']))
  fireEvent.submit(screen.getByRole('form', { name: en['monitor.intervalEdit.form'] }))

  expect(fetchMock).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: en['monitor.intervalEdit.submit'] }))
    .toBeDisabled()
})

test('does not offer interval controls for an archived Monitor', () => {
  renderForm({ ...monitor, state: 'archived' })

  expect(screen.queryByRole('form', { name: en['monitor.intervalEdit.form'] }))
    .not.toBeInTheDocument()
})
