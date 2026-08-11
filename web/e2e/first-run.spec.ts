import { expect, test, type Page } from '@playwright/test'

const administratorEmail = 'admin@example.test'
const administratorPassword = 'a-long-admin-password'

interface CreatedMonitor {
  id: string
  organizationId: string
  projectId: string
}

async function unsafeJSON<T>(
  page: Page,
  method: 'POST' | 'PUT',
  path: string,
  data: unknown,
): Promise<T> {
  const tokenResponse = await page.request.get('/api/v1/auth/antiforgery')
  expect(tokenResponse.ok()).toBe(true)
  const token = await tokenResponse.json() as {
    headerName: string
    requestToken: string
  }
  const response = await page.request.fetch(path, {
    method,
    data,
    headers: {
      Origin: 'http://127.0.0.1:5173',
      [token.headerName]: token.requestToken,
    },
  })
  if (!response.ok()) {
    throw new Error(
      `${method} ${path} returned ${response.status()}: ${await response.text()}`,
    )
  }
  return await response.json() as T
}

// The first critical journey: a fresh installation is set up and is immediately
// usable, the administrator signs out and back in, and provisions a second
// Organization.
test('first run: setup lands on a provisioned Organization, then sign in and add another', async ({ page }) => {
  test.setTimeout(240_000)
  // A fresh installation routes every visitor to first-administrator setup.
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByRole('heading', { name: 'Set up ProbeHive' })).toBeVisible()

  await page.getByLabel('Email').fill(administratorEmail)
  await page.getByLabel('Display name').fill('First Administrator')
  await page.getByLabel('Password').fill(administratorPassword)
  await page.getByRole('button', { name: 'Create administrator' }).click()

  // Setup signs the administrator in and provisions the installation Organization,
  // so the journey lands on it instead of a create-an-Organization step.
  await expect(page).toHaveURL(/\/organizations\/[0-9a-f-]+$/)
  await expect(page.getByRole('heading', { name: 'Default', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Default Project' })).toBeVisible()
  await expect(page.getByText(administratorEmail)).toBeVisible()

  // The installation Organization is named Default until it is renamed.
  await page.getByRole('textbox', { name: 'Display name' }).fill('My Services')
  await page.getByRole('button', { name: 'Rename', exact: true }).click()
  await expect(page.getByText('Organization renamed.')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'My Services' })).toBeVisible()
  // The slug is immutable, so it still reads default.
  await expect(page.getByText('default', { exact: true })).toBeVisible()

  // The default Project can immediately hold a fully configured HTTP Monitor.
  await page.getByLabel('Monitor name').fill('ProbeHive API')
  await page.getByLabel('Target URL').fill('http://127.0.0.1:5080/readyz')
  const monitorResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    response.url().endsWith('/monitors') &&
    response.status() === 201,
  )
  await page.getByRole('button', { name: 'Create and activate' }).click()
  const createdMonitor = await (await monitorResponse).json() as CreatedMonitor
  await expect(page.getByText('ProbeHive API is active.')).toBeVisible()
  await expect(page.getByRole('row', { name: /ProbeHive API Active/ })).toBeVisible()

  await page.getByRole('link', { name: 'ProbeHive API' }).click()
  const renameMonitor = page.getByRole('region', { name: 'Rename Monitor' })
  await renameMonitor.getByLabel('Monitor name').fill('ProbeHive Readiness')
  await renameMonitor.getByRole('button', { name: 'Rename', exact: true }).click()
  await expect(renameMonitor.getByText('Monitor renamed.')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'ProbeHive Readiness' })).toBeVisible()
  const executionInterval = page.getByRole('region', { name: 'Execution interval' })
  await executionInterval.getByLabel('Interval (seconds)').fill('300')
  await executionInterval.getByRole('button', { name: 'Update interval' }).click()
  await expect(executionInterval.getByText('Execution interval updated.')).toBeVisible()
  await expect(page.getByText('300 seconds', { exact: true })).toBeVisible()
  const lifecycle = page.getByRole('region', { name: 'Lifecycle' })
  await lifecycle.getByRole('button', { name: 'Pause', exact: true }).click()
  await expect(lifecycle.getByText('Monitor paused.')).toBeVisible()
  await expect(page.getByText('Paused', { exact: true })).toBeVisible()
  await lifecycle.getByRole('button', { name: 'Activate', exact: true }).click()
  await expect(lifecycle.getByText('Monitor activated.')).toBeVisible()
  await expect(page.getByText('Active', { exact: true })).toBeVisible()

  const maintenance = page.getByRole('region', { name: 'Maintenance windows' })
  const maintenanceStart = new Date(Date.now() + 15 * 60_000)
  maintenanceStart.setUTCSeconds(0, 0)
  const maintenanceEnd = new Date(maintenanceStart.getTime() + 60 * 60_000)
  await maintenance.getByLabel('Start (UTC)').fill(maintenanceStart.toISOString().slice(0, 16))
  await maintenance.getByLabel('End (UTC)').fill(maintenanceEnd.toISOString().slice(0, 16))
  const maintenanceResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    response.url().endsWith('/maintenance-windows') &&
    response.status() === 201,
  )
  await maintenance.getByRole('button', { name: 'Schedule window' }).click()
  const createdWindow = await (await maintenanceResponse).json() as { id: string }
  await expect(maintenance.getByText('Maintenance window scheduled.')).toBeVisible()
  await expect(maintenance.getByText('Upcoming')).toBeVisible()
  const cancellationResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    response.url().endsWith('/maintenance-windows/' + createdWindow.id + '/cancel') &&
    response.status() === 200,
  )
  await maintenance.getByRole('button', { name: 'Cancel window' }).click()
  await cancellationResponse
  await expect(maintenance.getByText('Maintenance window cancelled.')).toBeVisible()
  await expect(maintenance.getByText('Cancelled', { exact: true })).toBeVisible()

  // The detail and inventory use distinct query keys. Returning to the
  // Organization proves successful mutations refreshed both views.
  await page.getByRole('link', { name: 'Back to Organization' }).click()
  const inventoryRow = page.getByRole('row', { name: /ProbeHive Readiness Active/ })
  await expect(inventoryRow).toBeVisible()
  await expect(inventoryRow.getByText('300 seconds')).toBeVisible()

  // Trigger a completed manual Run through the operator UI and follow its direct
  // navigation into the existing Run -> Observation evidence path.
  await page.getByRole('link', { name: 'ProbeHive Readiness' }).click()
  await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Health' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Incidents' })).toBeVisible()
  const manualRunResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    response.url().endsWith(`/monitors/${createdMonitor.id}/runs`) &&
    response.status() === 201,
  )
  await page.getByRole('button', { name: 'Run now' }).click()
  const manualRun = await (await manualRunResponse).json() as {
    id: string
    kind: string
    outcome: string
  }
  expect(manualRun.kind).toBe('manual')
  expect(manualRun.outcome).toBe('passed')
  await expect(page).toHaveURL(new RegExp(`/runs/${manualRun.id}$`))
  await expect(page.getByRole('heading', { name: 'Run evidence' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Observation' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'HTTP response' })).toBeVisible()
  await expect(page.getByText('200', { exact: true })).toBeVisible()
  await expect(page.getByText('No TLS session was recorded.')).toBeVisible()

  // Reloading proves the scoped Run URL is a real deep link. It also resets the
  // application's antiforgery cache after the out-of-band seeding request above.
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Run evidence' })).toBeVisible()

  // A scheduled failure plus its confirmation creates an open Incident. Poll the
  // real API for that durable state, then acknowledge it through the browser UI.
  await page.getByRole('link', { name: 'Back to Organization' }).click()
  const organizationAPI = `/api/v1/organizations/${createdMonitor.organizationId}`
  const monitorsAPI = `${organizationAPI}/projects/${createdMonitor.projectId}/monitors`
  const webhook = await unsafeJSON<{
    integration: { id: string; version: number }
  }>(page, 'POST', `${organizationAPI}/webhook-integrations`, {
    name: 'Maintenance receiver',
    destinationUrl: 'https://127.0.0.1:5080/webhook',
  })
  await unsafeJSON(page, 'PUT',
    `${organizationAPI}/webhook-integrations/${webhook.integration.id}/state`, {
      enabled: true,
      version: webhook.integration.version,
    },
  )
  const failingMonitor = await unsafeJSON<CreatedMonitor>(page, 'POST', monitorsAPI, {
    name: 'Unavailable Service',
    checkType: 'http',
    intervalSeconds: 30,
  })
  const failingMonitorAPI = `${monitorsAPI}/${failingMonitor.id}`
  await unsafeJSON(page, 'POST', `${failingMonitorAPI}/revisions`, {
    checkSchemaVersion: 1,
    checkConfiguration: { url: 'http://127.0.0.1:5080/not-found' },
  })
  const maintenanceStartsAt = new Date(Date.now() + 3_000)
  const maintainedWindow = await unsafeJSON<{ id: string }>(
    page, 'POST', `${failingMonitorAPI}/maintenance-windows`, {
      startsAt: maintenanceStartsAt.toISOString(),
      endsAt: new Date(maintenanceStartsAt.getTime() + 10 * 60_000).toISOString(),
    },
  )
  await expect.poll(() => Date.now(), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(maintenanceStartsAt.getTime())
  await unsafeJSON(page, 'PUT', `${failingMonitorAPI}/state`, { state: 'active' })
  await page.reload()
  await expect.poll(async () => page.evaluate(async (scope) => {
    const response = await fetch(
      `/api/v1/organizations/${scope.organizationId}/projects/${scope.projectId}` +
      `/monitors/${scope.id}/incidents?pageSize=1`,
    )
    const body = await response.json() as { items: unknown[] }
    return body.items.length
  }, failingMonitor), { timeout: 60_000 }).toBe(1)
  await expect.poll(async () => page.evaluate(async (scope) => {
    const response = await fetch(
      `/api/v1/organizations/${scope.organizationId}/projects/${scope.projectId}` +
      `/monitors/${scope.id}/alerts?pageSize=1`,
    )
    const body = await response.json() as { items: unknown[] }
    return body.items.length
  }, failingMonitor), { timeout: 60_000 }).toBe(1)

  await page.getByRole('link', { name: 'Unavailable Service' }).click()
  const alertIntents = page.getByRole('region', { name: 'Alert intents' })
  const openedAlertRow = alertIntents.getByRole('row').filter({ hasText: 'Incident opened' })
  await expect(openedAlertRow).toBeVisible()
  await openedAlertRow.getByRole('button', { name: 'Show delivery evidence' }).click()
  await expect(alertIntents.getByRole('heading', { name: 'Delivery evidence' })).toBeVisible()
  await expect(
    alertIntents.getByText('Suppressed by Monitor maintenance at the Alert occurrence time. No Webhook call was made.'),
  ).toBeVisible()
  await expect(alertIntents.getByText(maintainedWindow.id)).toBeVisible()
  await openedAlertRow.getByRole('link', { name: 'View source Incident' }).click()
  const incidentDetail = page.getByRole('region', { name: 'Incident evidence' })
  await incidentDetail.getByRole('button', { name: 'Acknowledge Incident' }).click()
  await expect(incidentDetail.getByText('Incident acknowledged.')).toBeVisible()
  await expect(incidentDetail.locator('.incident-state')).toHaveText('Acknowledged')
  await expect(incidentDetail.locator('.incident-timeline-heading strong').last()).toHaveText('Acknowledged')

  // Replace the failed target through a later immutable revision, then observe
  // the scheduled and confirmation Runs resolve the acknowledged Incident.
  const target = page.getByRole('region', { name: 'Target URL' })
  await target.getByLabel('New target URL').fill('http://127.0.0.1:5080/readyz')
  const revisionResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    response.url().endsWith('/monitors/' + failingMonitor.id + '/revisions') &&
    response.status() === 201,
  )
  await target.getByRole('button', { name: 'Replace target' }).click()
  const replacementRevision = await (await revisionResponse).json() as {
    revisionNumber: number
  }
  expect(replacementRevision.revisionNumber).toBe(2)
  await expect(target.getByText('Monitor target replaced in revision v2.')).toBeVisible()
  await expect(page.locator('.monitor-summary').getByText('v2', { exact: true })).toBeVisible()

  await expect.poll(async () => page.evaluate(async (scope) => {
    const response = await fetch(
      '/api/v1/organizations/' + scope.organizationId + '/projects/' + scope.projectId +
      '/monitors/' + scope.id + '/incidents?pageSize=1',
    )
    const body = await response.json() as { items: Array<{ state: string }> }
    return body.items[0]?.state
  }, failingMonitor), { timeout: 90_000 }).toBe('resolved')

  await expect.poll(async () => page.evaluate(async (scope) => {
    const response = await fetch(
      `/api/v1/organizations/${scope.organizationId}/projects/${scope.projectId}` +
      `/monitors/${scope.id}/alerts?pageSize=2`,
    )
    const body = await response.json() as { items: unknown[] }
    return body.items.length
  }, failingMonitor), { timeout: 60_000 }).toBe(2)

  await page.reload()
  const resolvedIncident = page.getByRole('region', { name: 'Incident evidence' })
  await expect(resolvedIncident.locator('.incident-state')).toHaveText('Resolved')
  await expect(
    resolvedIncident.locator('.incident-timeline-heading strong').last(),
  ).toHaveText('Resolved')
  const resolvedAlertRow = page.getByRole('region', { name: 'Alert intents' })
    .getByRole('row').filter({ hasText: 'Incident resolved' })
  await expect(resolvedAlertRow).toBeVisible()

  // Sign out to exercise the login journey with the created credentials.
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()

  await page.getByLabel('Email').fill(administratorEmail)
  await page.getByLabel('Password').fill(administratorPassword)
  await page.getByRole('button', { name: 'Sign in' }).click()

  // Signing in lands on the Organization list, which already holds the provisioned one.
  await expect(page.getByRole('heading', { name: 'Organizations' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'My Services' })).toBeVisible()

  // Provisioning an additional Organization is still reachable from that page.
  await page.getByLabel('Slug').fill('acme')
  await page.getByLabel('Display name').fill('Acme Monitoring')
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page.getByText('Organization created.')).toBeVisible()

  await page.getByRole('link', { name: 'View Acme Monitoring' }).click()
  await expect(page).toHaveURL(/\/organizations\/[0-9a-f-]+$/)
  await expect(page.getByRole('heading', { name: 'Acme Monitoring' })).toBeVisible()
  await expect(page.getByText('acme', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Default Project' })).toBeVisible()
})

test('the interface can be switched to another language', async ({ page }) => {
  // Runs after the first-run journey, so the installation already has an administrator.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()

  await page.getByLabel('Language').selectOption('zh-CN')

  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')

  // The preference survives a reload rather than renegotiating from the browser.
  await page.reload()
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible()
})
