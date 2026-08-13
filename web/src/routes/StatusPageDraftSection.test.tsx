import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { MonitorResponse } from '../api/monitors'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders'
import StatusPageDraftSection from './StatusPageDraftSection'

const organizationId = '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee'
const projectId = '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420'
const draftURL = `/api/v1/organizations/${organizationId}/status-page/draft`
const monitorsURL = `/api/v1/organizations/${organizationId}/projects/${projectId}/monitors`

const monitors: MonitorResponse[] = [
  {
    id: '019f8f3d-5bb0-7000-8000-000000000001',
    organizationId,
    projectId,
    name: 'API Internal Name',
    checkType: 'http',
    state: 'active',
    intervalSeconds: 60,
    latestRevisionNumber: 1,
    createdAt: '2026-08-12T00:00:00Z',
    updatedAt: '2026-08-12T00:00:00Z',
  },
  {
    id: '019f8f3d-5bb0-7000-8000-000000000002',
    organizationId,
    projectId,
    name: 'Website Internal Name',
    checkType: 'http',
    state: 'paused',
    intervalSeconds: 60,
    latestRevisionNumber: 1,
    createdAt: '2026-08-12T00:00:00Z',
    updatedAt: '2026-08-12T00:00:00Z',
  },
  {
    id: '019f8f3d-5bb0-7000-8000-000000000003',
    organizationId,
    projectId,
    name: 'Archived',
    checkType: 'http',
    state: 'archived',
    intervalSeconds: 60,
    latestRevisionNumber: 1,
    createdAt: '2026-08-12T00:00:00Z',
    updatedAt: '2026-08-12T00:00:00Z',
  },
]

afterEach(() => {
  vi.restoreAllMocks()
})

function renderSection() {
  return renderWithProviders(
    <StatusPageDraftSection organizationId={organizationId} projectId={projectId} />,
    '',
    '/',
  )
}

test('creates a private draft from explicit Monitor selections and public labels', async () => {
  const user = userEvent.setup()
  let savedBody: unknown
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url === draftURL && init?.method === 'PUT') {
      savedBody = JSON.parse(init.body as string)
      return jsonResponse(200, {
        id: '019f8f3d-5bb0-7000-8000-000000000010',
        organizationId,
        title: 'Service Status',
        version: 1,
        components: [
          { id: 'component-2', monitorId: monitors[1]!.id, label: 'Website', position: 0 },
          { id: 'component-1', monitorId: monitors[0]!.id, label: 'Public API', position: 1 },
        ],
        createdAt: '2026-08-12T00:00:00Z',
        updatedAt: '2026-08-12T00:00:00Z',
      })
    }
    if (url === draftURL) {
      return new Response(null, { status: 204 })
    }
    if (url === monitorsURL) {
      return jsonResponse(200, monitors)
    }
    throw new Error(`Unexpected fetch: ${url}`)
  })
  renderSection()

  const form = await screen.findByRole('form', { name: en['statusPage.form'] })
  expect(within(form).queryByText('Archived')).not.toBeInTheDocument()
  await user.type(within(form).getByLabelText(en['statusPage.title']), 'Service Status')
  await user.click(within(form).getByRole('checkbox', { name: 'API Internal Name' }))
  await user.click(within(form).getByRole('checkbox', { name: 'Website Internal Name' }))
  const labels = within(form).getAllByLabelText(en['statusPage.publicLabel'])
  await user.clear(labels[0]!)
  await user.type(labels[0]!, 'Public API')
  await user.clear(labels[1]!)
  await user.type(labels[1]!, 'Website')
  await user.click(within(form).getByRole('button', { name: 'Move Website up' }))
  await user.click(within(form).getByRole('button', { name: en['statusPage.save'] }))

  expect(await screen.findByText(en['statusPage.saved'])).toBeInTheDocument()
  expect(savedBody).toEqual({
    title: 'Service Status',
    version: 0,
    components: [
      { monitorId: monitors[1]!.id, label: 'Website' },
      { monitorId: monitors[0]!.id, label: 'Public API' },
    ],
  })
  expect(fetchMock).toHaveBeenCalledWith(draftURL, expect.objectContaining({ method: 'PUT' }))
})

test('loads an existing order and reports administrator-only access', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url === draftURL) {
      return jsonResponse(200, {
        id: 'page', organizationId, title: 'Existing', version: 4,
        components: [
          { id: 'b', monitorId: monitors[1]!.id, label: 'Second first', position: 0 },
          { id: 'a', monitorId: monitors[0]!.id, label: 'First second', position: 1 },
        ],
        createdAt: '2026-08-12T00:00:00Z', updatedAt: '2026-08-12T00:00:00Z',
      })
    }
    if (url === monitorsURL) return jsonResponse(200, monitors)
    throw new Error(`Unexpected fetch: ${url}`)
  })
  renderSection()
  const form = await screen.findByRole('form', { name: en['statusPage.form'] })
  expect(within(form).getByLabelText(en['statusPage.title'])).toHaveValue('Existing')
  expect(within(form).getAllByLabelText(en['statusPage.publicLabel']).map((input) =>
    (input as HTMLInputElement).value,
  )).toEqual(['Second first', 'First second'])

  vi.restoreAllMocks()
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(403, { status: 403 }))
  renderSection()
  expect(await screen.findByText(en['statusPage.administratorOnly'])).toBeInTheDocument()
})

test('removes a component whose Monitor is no longer available', async () => {
  const user = userEvent.setup()
  let savedBody: unknown
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url === draftURL && init?.method === 'PUT') {
      savedBody = JSON.parse(init.body as string)
      return jsonResponse(200, {
        id: 'page', organizationId, title: 'Existing', version: 5,
        components: [
          { id: 'a', monitorId: monitors[0]!.id, label: 'API', position: 0 },
        ],
        createdAt: '2026-08-12T00:00:00Z', updatedAt: '2026-08-12T00:01:00Z',
      })
    }
    if (url === draftURL) {
      return jsonResponse(200, {
        id: 'page', organizationId, title: 'Existing', version: 4,
        components: [
          { id: 'a', monitorId: monitors[0]!.id, label: 'API', position: 0 },
          { id: 'c', monitorId: monitors[2]!.id, label: 'Retired', position: 1 },
        ],
        createdAt: '2026-08-12T00:00:00Z', updatedAt: '2026-08-12T00:00:00Z',
      })
    }
    if (url === monitorsURL) return jsonResponse(200, monitors)
    throw new Error(`Unexpected fetch: ${url}`)
  })
  renderSection()
  const form = await screen.findByRole('form', { name: en['statusPage.form'] })
  await user.click(within(form).getByRole('button', { name: 'Remove Retired' }))
  await user.click(within(form).getByRole('button', { name: en['statusPage.save'] }))

  expect(await screen.findByText(en['statusPage.saved'])).toBeInTheDocument()
  expect(savedBody).toEqual({
    title: 'Existing', version: 4,
    components: [{ monitorId: monitors[0]!.id, label: 'API' }],
  })
})

test('publishes a one-time anonymous URL and revokes it', async () => {
  const user = userEvent.setup()
  const publicationURL = `/api/v1/organizations/${organizationId}/status-page/publication`
  const publicURL = 'https://status.example/status/opaque-token'
  let published = false
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url === publicationURL && init?.method === 'POST') {
      published = true
      return jsonResponse(201, { publicUrl: publicURL, publishedAt: '2026-08-13T00:00:00Z' })
    }
    if (url === publicationURL && init?.method === 'DELETE') {
      published = false
      return new Response(null, { status: 204 })
    }
    if (url === draftURL) {
      return jsonResponse(200, {
        id: 'page', organizationId, title: 'Service Status', version: 1,
        components: [{ id: 'a', monitorId: monitors[0]!.id, label: 'Public API', position: 0 }],
        publication: published ? { publishedAt: '2026-08-13T00:00:00Z' } : null,
        createdAt: '2026-08-12T00:00:00Z', updatedAt: '2026-08-12T00:00:00Z',
      })
    }
    if (url === monitorsURL) return jsonResponse(200, monitors)
    throw new Error(`Unexpected fetch: ${url}`)
  })
  renderSection()

  const form = await screen.findByRole('form', { name: en['statusPage.form'] })
  await user.click(within(form).getByRole('button', { name: en['statusPage.publication.publish'] }))
  expect(await within(form).findByRole('link', { name: publicURL })).toHaveAttribute('href', publicURL)
  expect(within(form).getByText(en['statusPage.publication.once'])).toBeInTheDocument()

  await user.click(within(form).getByRole('button', { name: en['statusPage.publication.revoke'] }))
  expect(await within(form).findByText(en['statusPage.publication.revoked'])).toBeInTheDocument()
  expect(within(form).queryByRole('link', { name: publicURL })).not.toBeInTheDocument()
  expect(within(form).getByRole('button', { name: en['statusPage.publication.publish'] })).toBeEnabled()
})
