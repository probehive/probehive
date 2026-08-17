import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { setAntiforgeryForTests } from '../api/http'
import type { OrganizationResponse } from '../api/organizations'
import type { WebhookIntegrationResponse } from '../api/webhooks'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import IntegrationsPage from './IntegrationsPage.tsx'

const organization: OrganizationResponse = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  slug: 'acme',
  displayName: 'Acme Monitoring',
  createdAt: '2026-07-23T12:00:00+00:00',
  defaultProject: {
    id: '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420',
    organizationId: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
    name: 'Default',
    isDefault: true,
    createdAt: '2026-07-23T12:00:00+00:00',
  },
}

function integration(overrides: Partial<WebhookIntegrationResponse> = {}): WebhookIntegrationResponse {
  return {
    id: '019f8f3d-5bb0-7ddd-8000-02811ef6b2ee',
    organizationId: organization.id,
    name: 'Primary receiver',
    destinationUrl: 'https://hooks.example.test/events',
    enabled: false,
    version: 1,
    activeSecretVersion: 1,
    pendingSecretVersion: null,
    retiringSecretVersion: null,
    createdAt: '2026-08-17T01:00:00+00:00',
    updatedAt: '2026-08-17T01:00:00+00:00',
    ...overrides,
  }
}

function generatedSigningSecret(): string {
  const suffix = Array.from({ length: 43 }, (_, index) =>
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-'[index % 64],
  ).join('')
  return 'phwh_' + suffix
}

function renderPage(locale = 'en') {
  return renderWithProviders(
    <IntegrationsPage />,
    'organizations/:organizationId/integrations',
    '/organizations/' + organization.id + '/integrations',
    { locale },
  )
}

interface MockOptions {
  initial?: WebhookIntegrationResponse[]
  listStatus?: number
}

function mockWebhookAPI(options: MockOptions = {}) {
  let values = [...(options.initial ?? [])]
  const signingSecrets: string[] = []
  const root = '/api/v1/organizations/' + organization.id + '/webhook-integrations'
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/organizations/' + organization.id && method === 'GET') {
        return jsonResponse(200, organization)
      }
      if (url === root && method === 'GET') {
        return options.listStatus && options.listStatus !== 200
          ? jsonResponse(options.listStatus, { status: options.listStatus })
          : jsonResponse(200, values)
      }
      if (url === root && method === 'POST') {
        const body = JSON.parse(String(init?.body)) as { name: string; destinationUrl: string }
        const created = integration({
          id: '019f8f3d-5bb0-7eee-8000-02811ef6b2ee',
          name: body.name,
          destinationUrl: body.destinationUrl,
        })
        values = [...values, created]
        const signingSecret = generatedSigningSecret()
        signingSecrets.push(signingSecret)
        return jsonResponse(201, { integration: created, signingSecret })
      }
      const current = values.find((value) => url.includes('/' + value.id + '/'))
      if (current && url.endsWith('/state') && method === 'PUT') {
        const body = JSON.parse(String(init?.body)) as { enabled: boolean; version: number }
        expect(body.version).toBe(current.version)
        const updated = { ...current, enabled: body.enabled, version: current.version + 1 }
        values = values.map((value) => value.id === updated.id ? updated : value)
        return jsonResponse(200, updated)
      }
      if (current && url.endsWith('/signing-secrets/prepare') && method === 'POST') {
        const nextVersion = current.activeSecretVersion + 1
        const updated = {
          ...current,
          version: current.version + 1,
          pendingSecretVersion: nextVersion,
        }
        values = values.map((value) => value.id === updated.id ? updated : value)
        const signingSecret = generatedSigningSecret()
        signingSecrets.push(signingSecret)
        return jsonResponse(201, {
          integration: updated,
          secretVersion: nextVersion,
          signingSecret,
        })
      }
      if (current && url.endsWith('/signing-secrets/activate') && method === 'POST') {
        const updated = {
          ...current,
          version: current.version + 1,
          activeSecretVersion: current.pendingSecretVersion,
          pendingSecretVersion: null,
          retiringSecretVersion: current.activeSecretVersion,
        } as WebhookIntegrationResponse
        values = values.map((value) => value.id === updated.id ? updated : value)
        return jsonResponse(200, updated)
      }
      if (current && url.endsWith('/signing-secrets/retire') && method === 'POST') {
        const updated = {
          ...current,
          version: current.version + 1,
          retiringSecretVersion: null,
        }
        values = values.map((value) => value.id === updated.id ? updated : value)
        return jsonResponse(200, updated)
      }
      return Promise.reject(new Error('Unexpected fetch: ' + method + ' ' + url))
    },
  )
  return {
    fetchMock,
    get values() {
      return values
    },
    get signingSecrets() {
      return signingSecrets
    },
  }
}

beforeEach(() => {
  setAntiforgeryForTests({
    headerName: 'X-ProbeHive-Antiforgery',
    requestToken: 'test-token',
  })
})

afterEach(() => {
  setAntiforgeryForTests(null)
  vi.restoreAllMocks()
})

test('renders the empty state and creation form on the Organization route', async () => {
  mockWebhookAPI()
  renderPage()

  expect(await screen.findByRole('heading', { name: 'Acme Monitoring' })).toBeInTheDocument()
  expect(await screen.findByText('No Webhook integrations yet.')).toBeInTheDocument()
  expect(screen.getByRole('form', { name: 'Create Webhook integration' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Back to Organization' })).toHaveAttribute(
    'href',
    '/organizations/' + organization.id,
  )
})

test('renders a stable administrator-only permission state', async () => {
  mockWebhookAPI({ listStatus: 403 })
  renderPage()

  expect(
    await screen.findByText('Only an Organization administrator can manage Webhook integrations.'),
  ).toBeInTheDocument()
  expect(screen.queryByRole('form', { name: 'Create Webhook integration' })).not.toBeInTheDocument()
})

test('renders a retryable stable error state', async () => {
  const api = mockWebhookAPI({ listStatus: 500 })
  renderPage()

  expect(await screen.findByText('The Webhook integrations could not be loaded.')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => {
    expect(api.fetchMock).toHaveBeenCalledWith(
      '/api/v1/organizations/' + organization.id + '/webhook-integrations',
    )
  })
})

test('creates an Integration and keeps its secret only in the current result state', async () => {
  const api = mockWebhookAPI()
  const user = userEvent.setup()
  const firstRender = renderPage()

  await screen.findByText('No Webhook integrations yet.')
  await user.type(screen.getByLabelText('Name'), 'Alert receiver')
  await user.type(screen.getByLabelText('Destination URL'), 'https://hooks.example.test/alerts')
  await user.click(screen.getByRole('button', { name: 'Create integration' }))

  await screen.findByRole('heading', { name: 'Signing secret' })
  const signingSecret = api.signingSecrets[0] as string
  expect(screen.getByTestId('one-time-secret')).toHaveTextContent(signingSecret)
  expect(await screen.findByText('Alert receiver')).toBeInTheDocument()
  expect(globalThis.location.href).not.toContain(signingSecret)
  for (let index = 0; index < globalThis.localStorage.length; index++) {
    const key = globalThis.localStorage.key(index)
    expect(key === null ? null : globalThis.localStorage.getItem(key)).not.toContain(signingSecret)
  }

  firstRender.unmount()
  renderPage()
  expect(await screen.findByText('Alert receiver')).toBeInTheDocument()
  expect(screen.queryByTestId('one-time-secret')).not.toBeInTheDocument()
})

test('requires explicit confirmation before enabling and reports success', async () => {
  const api = mockWebhookAPI({ initial: [integration()] })
  const user = userEvent.setup()
  renderPage()

  await screen.findByText('Primary receiver')
  await user.click(screen.getByRole('button', { name: 'Enable' }))
  expect(screen.getByText('Enable Primary receiver for future Alert routing?')).toBeInTheDocument()
  expect(api.values[0]?.enabled).toBe(false)

  await user.click(screen.getByRole('button', { name: 'Confirm enable' }))
  expect(await screen.findByText('Primary receiver is enabled.')).toBeInTheDocument()
  expect(screen.getByText('Enabled')).toBeInTheDocument()
  expect(api.values[0]?.enabled).toBe(true)
})

test('recovers each signing-secret rotation phase after a reload', async () => {
  const api = mockWebhookAPI({ initial: [integration()] })
  const user = userEvent.setup()
  const firstRender = renderPage()

  await screen.findByText('Primary receiver')
  await user.click(screen.getByRole('button', { name: 'Prepare new secret' }))
  expect(screen.getByText(/Prepare a new signing secret/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Prepare secret' }))
  await screen.findByRole('heading', { name: 'Signing secret' })
  const preparedSecret = api.signingSecrets[0] as string
  expect(screen.getByTestId('one-time-secret')).toHaveTextContent(preparedSecret)
  expect(screen.getByText('Secret v2 pending activation')).toBeInTheDocument()

  firstRender.unmount()
  const secondRender = renderPage()
  expect(await screen.findByText('Secret v2 pending activation')).toBeInTheDocument()
  expect(screen.queryByText(preparedSecret)).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Activate new secret' }))
  expect(screen.getByText(/Activate secret version 2/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Confirm activation' }))
  expect(await screen.findByText('The new signing secret is active for Primary receiver.')).toBeInTheDocument()
  expect(screen.getByText('Secret v1 awaiting retirement')).toBeInTheDocument()

  secondRender.unmount()
  renderPage()
  expect(await screen.findByText('Secret v1 awaiting retirement')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Retire old secret' }))
  expect(screen.getByText(/Retire secret version 1/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Confirm retirement' }))
  expect(await screen.findByText('The old signing secret was retired for Primary receiver.')).toBeInTheDocument()
  expect(screen.queryByText(/awaiting retirement/)).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Prepare new secret' })).toBeInTheDocument()
})

test('renders the management workflow in Simplified Chinese', async () => {
  mockWebhookAPI()
  renderPage('zh-CN')

  expect(await screen.findByText('还没有 Webhook 集成。')).toBeInTheDocument()
  expect(screen.getByRole('form', { name: '创建 Webhook 集成' })).toBeInTheDocument()
})
