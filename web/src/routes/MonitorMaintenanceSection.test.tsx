import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MaintenanceWindowResponse } from '../api/maintenance'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders'
import MonitorMaintenanceSection from './MonitorMaintenanceSection'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const monitorId = '019f8f3d-5bb0-7cf5-b971-3c752c712310'
const collection =
  '/api/v1/organizations/' + organizationId + '/projects/' + projectId +
  '/monitors/' + monitorId + '/maintenance-windows'

const upcoming: MaintenanceWindowResponse = {
  id: '019f8f3d-5bb0-7a10-9000-000000000001',
  organizationId,
  projectId,
  monitorId,
  startsAt: '2099-08-10T13:00:00Z',
  endsAt: '2099-08-10T14:00:00Z',
  status: 'upcoming',
  createdAt: '2099-08-10T12:00:00Z',
  cancelledAt: null,
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderSection() {
  return renderWithProviders(
    <MonitorMaintenanceSection
      organizationId={organizationId}
      projectId={projectId}
      monitorId={monitorId}
    />,
    '',
    '/',
  )
}

test('lists and cancels an upcoming maintenance window', async () => {
  const cancelled: MaintenanceWindowResponse = {
    ...upcoming,
    status: 'cancelled',
    cancelledAt: '2099-08-10T12:15:00Z',
  }
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((_input, init) => {
    if (init?.method === 'POST') {
      return Promise.resolve(jsonResponse(200, cancelled))
    }
    return Promise.resolve(jsonResponse(200, [upcoming]))
  })
  renderSection()

  expect(await screen.findByText(en['maintenance.status.upcoming'])).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: en['maintenance.cancel.submit'] }))

  expect(await screen.findByText(en['maintenance.status.cancelled'])).toBeInTheDocument()
  expect(screen.getByText(en['maintenance.cancel.done'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    collection + '/' + upcoming.id + '/cancel',
    expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-ProbeHive-Antiforgery': 'test-token' }),
    }),
  )
})

test('serializes explicit UTC bounds when scheduling a window', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((_input, init) => {
    if (init?.method === 'POST') {
      return Promise.resolve(jsonResponse(201, upcoming))
    }
    return Promise.resolve(jsonResponse(200, []))
  })
  renderSection()
  await screen.findByText(en['maintenance.empty'])

  fireEvent.change(screen.getByLabelText(en['maintenance.startsAt']), {
    target: { value: '2099-08-10T13:00' },
  })
  fireEvent.change(screen.getByLabelText(en['maintenance.endsAt']), {
    target: { value: '2099-08-10T14:00' },
  })
  await userEvent.click(screen.getByRole('button', { name: en['maintenance.create.submit'] }))

  expect(await screen.findByText(en['maintenance.create.done'])).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
    collection,
    expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        startsAt: '2099-08-10T13:00:00.000Z',
        endsAt: '2099-08-10T14:00:00.000Z',
      }),
    }),
  ))
})

test('localizes a stable maintenance validation code', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((_input, init) => {
    if (init?.method === 'POST') {
      return Promise.resolve(jsonResponse(400, {
        title: 'server wording that must not appear',
        status: 400,
        errors: {
          startsAt: [{
            code: 'maintenance.startsAt.invalid',
            message: 'server wording that must not appear',
          }],
        },
      }))
    }
    return Promise.resolve(jsonResponse(200, []))
  })
  renderSection()
  await screen.findByText(en['maintenance.empty'])

  fireEvent.change(screen.getByLabelText(en['maintenance.startsAt']), {
    target: { value: '2099-08-10T13:00' },
  })
  fireEvent.change(screen.getByLabelText(en['maintenance.endsAt']), {
    target: { value: '2099-08-10T14:00' },
  })
  await userEvent.click(screen.getByRole('button', { name: en['maintenance.create.submit'] }))

  expect(await screen.findByText(en['error.maintenance.startsAt.invalid'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})
