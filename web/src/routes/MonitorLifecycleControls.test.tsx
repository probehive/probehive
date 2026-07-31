import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorResponse } from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import MonitorLifecycleControls from './MonitorLifecycleControls.tsx'
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

const stateURL =
  `/api/v1/organizations/${monitor.organizationId}/projects/${monitor.projectId}` +
  `/monitors/${monitor.id}/state`

afterEach(() => {
  vi.restoreAllMocks()
})

function renderControls(value = monitor) {
  return renderWithProviders(<MonitorLifecycleControls monitor={value} />, '', '/')
}

test('pauses an active Monitor and synchronizes detail and inventory query state', async () => {
  const paused = {
    ...monitor,
    state: 'paused' as const,
    updatedAt: '2026-07-30T03:00:00Z',
  }
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, paused))
  const { queryClient } = renderControls()
  const listKey = monitorsQueryKey(monitor.organizationId, monitor.projectId)
  const detailKey = monitorQueryKey(monitor.organizationId, monitor.projectId, monitor.id)
  queryClient.setQueryData(listKey, [monitor])
  queryClient.setQueryData(detailKey, monitor)

  await userEvent.setup().click(screen.getByRole('button', {
    name: en['monitor.lifecycle.pause'],
  }))

  expect(await screen.findByText(en['monitor.lifecycle.paused'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    stateURL,
    expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
      body: JSON.stringify({ state: 'paused' }),
    }),
  )
  expect(queryClient.getQueryData(detailKey)).toEqual(paused)
  expect(queryClient.getQueryState(listKey)?.isInvalidated).toBe(true)
})

test('activates a paused Monitor', async () => {
  const paused = { ...monitor, state: 'paused' as const }
  const active = { ...paused, state: 'active' as const }
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, active))
  renderControls(paused)

  await userEvent.setup().click(screen.getByRole('button', {
    name: en['monitor.lifecycle.activate'],
  }))

  expect(await screen.findByText(en['monitor.lifecycle.activated'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    stateURL,
    expect.objectContaining({ body: JSON.stringify({ state: 'active' }) }),
  )
})

test('blocks activating a draft without a revision while preserving archive', () => {
  renderControls({ ...monitor, state: 'draft', latestRevisionNumber: 0 })

  expect(screen.getByRole('button', { name: en['monitor.lifecycle.activate'] })).toBeDisabled()
  expect(screen.getByText(en['monitor.lifecycle.activationBlocked'])).toBeInTheDocument()
  expect(screen.getByRole('button', { name: en['monitor.lifecycle.archive'] })).toBeEnabled()
})

test('requires confirmation before archiving a Monitor', async () => {
  const archived = { ...monitor, state: 'archived' as const }
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, archived))
  renderControls()
  const user = userEvent.setup()

  await user.click(screen.getByRole('button', { name: en['monitor.lifecycle.archive'] }))
  const confirmation = screen.getByRole('group', {
    name: en['monitor.lifecycle.archiveConfirmation'],
  })
  expect(fetchMock).not.toHaveBeenCalled()
  expect(confirmation).toHaveTextContent(en['monitor.lifecycle.archiveWarning'])

  await user.click(screen.getByRole('button', { name: en['monitor.lifecycle.cancel'] }))
  expect(screen.queryByRole('group', {
    name: en['monitor.lifecycle.archiveConfirmation'],
  })).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: en['monitor.lifecycle.archive'] }))
  await user.click(screen.getByRole('button', { name: en['monitor.lifecycle.confirmArchive'] }))

  expect(await screen.findByText(en['monitor.lifecycle.archived'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    stateURL,
    expect.objectContaining({ body: JSON.stringify({ state: 'archived' }) }),
  )
})

test('renders an archived Monitor as read-only', () => {
  renderControls({ ...monitor, state: 'archived' })

  expect(screen.getByText(en['monitor.lifecycle.archivedReadonly'])).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

test('localizes a rejected transition and leaves the action available for retry', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(409, {
    title: 'server wording that must not appear',
    status: 409,
    code: 'monitor.state.transitionNotAllowed',
  }))
  renderControls()

  await userEvent.setup().click(screen.getByRole('button', {
    name: en['monitor.lifecycle.pause'],
  }))

  expect(await screen.findByText(
    en['error.monitor.state.transitionNotAllowed'],
  )).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: en['monitor.lifecycle.pause'] })).toBeEnabled()
})

test('localizes authorization failures from the shared Problem Details catalog', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(403, {
    title: 'server wording that must not appear',
    status: 403,
    code: 'auth.forbidden',
  }))
  renderControls()

  await userEvent.setup().click(screen.getByRole('button', {
    name: en['monitor.lifecycle.pause'],
  }))

  expect(await screen.findByText(en['error.auth.forbidden'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})
