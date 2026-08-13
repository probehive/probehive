import { screen, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders'
import PublicStatusPage from './PublicStatusPage'

afterEach(() => vi.restoreAllMocks())

test('renders only current public labels, state, update time, and maintenance', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(200, {
    title: 'Service Status',
    components: [
      { label: 'Public API', state: 'degraded', updatedAt: '2026-08-13T00:00:00Z', maintenance: true },
      { label: 'Website', state: 'healthy', updatedAt: '2026-08-13T00:01:00Z', maintenance: false },
    ],
  }))
  renderWithProviders(<PublicStatusPage />, 'status/:publicationToken', '/status/opaque-token')

  expect(await screen.findByRole('heading', { name: 'Service Status' })).toBeInTheDocument()
  const api = screen.getByRole('listitem', { name: 'Public API' })
  expect(within(api).getByRole('heading', { name: 'Public API' })).toBeInTheDocument()
  expect(within(api).getByText(en['publicStatus.state.degraded'])).toBeInTheDocument()
  expect(within(api).getByText(en['publicStatus.maintenance'])).toBeInTheDocument()
  expect(screen.getByText(en['publicStatus.state.healthy'])).toBeInTheDocument()
  expect(screen.queryByText(/monitor|target|incident|uptime/i)).not.toBeInTheDocument()
})

test('uses the same unavailable presentation for missing or revoked pages', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(404, {
    status: 404, code: 'resource.notFound', title: 'Not Found',
  }))
  renderWithProviders(<PublicStatusPage />, 'status/:publicationToken', '/status/revoked')
  expect(await screen.findByText(en['publicStatus.notFound'])).toBeInTheDocument()
})
