import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { OrganizationResponse } from '../api/organizations'
import { en } from '../i18n/en'
import { jsonResponse, renderWithProviders } from '../test/renderWithProviders.tsx'
import RenameOrganizationForm from './RenameOrganizationForm.tsx'

const organization: OrganizationResponse = {
  id: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
  slug: 'default',
  displayName: 'Default',
  createdAt: '2026-07-27T12:00:00+00:00',
  defaultProject: {
    id: '019f8f3d-5bb0-7aa5-b81a-2868fc7c2420',
    organizationId: '019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee',
    name: 'Default',
    isDefault: true,
    createdAt: '2026-07-27T12:00:00+00:00',
  },
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderForm() {
  renderWithProviders(<RenameOrganizationForm organization={organization} />, '', '/')
}

test('prefills the current name and sends a PUT to the name endpoint', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url.startsWith('/api/v1/auth/antiforgery')) {
      return Promise.resolve(
        jsonResponse(200, { headerName: 'X-ProbeHive-Antiforgery', requestToken: 'token' }),
      )
    }
    return Promise.resolve(jsonResponse(200, { ...organization, displayName: 'Acme' }))
  })
  renderForm()

  const field = screen.getByLabelText(en['organization.rename.displayName'])
  expect(field).toHaveValue('Default')

  const user = userEvent.setup()
  await user.clear(field)
  await user.type(field, 'Acme')
  await user.click(screen.getByRole('button', { name: en['organization.rename.submit'] }))

  expect(await screen.findByText(en['organization.rename.done'])).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith(
    `/api/v1/organizations/${organization.id}/name`,
    expect.objectContaining({ method: 'PUT' }),
  )
})

test('localizes a rejected name from its code', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url.startsWith('/api/v1/auth/antiforgery')) {
      return Promise.resolve(
        jsonResponse(200, { headerName: 'X-ProbeHive-Antiforgery', requestToken: 'token' }),
      )
    }
    return Promise.resolve(
      jsonResponse(400, {
        title: 'One or more validation errors occurred.',
        status: 400,
        errors: {
          displayName: [
            { code: 'organization.displayName.invalid', message: 'server wording that must not appear' },
          ],
        },
      }),
    )
  })
  renderForm()

  const user = userEvent.setup()
  await user.clear(screen.getByLabelText(en['organization.rename.displayName']))
  await user.click(screen.getByRole('button', { name: en['organization.rename.submit'] }))

  expect(await screen.findByText(en['error.organization.displayName.invalid'])).toBeInTheDocument()
  expect(screen.queryByText('server wording that must not appear')).not.toBeInTheDocument()
})
