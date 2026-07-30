import { expect, test } from '@playwright/test'

const administratorEmail = 'admin@example.test'
const administratorPassword = 'a-long-admin-password'

// The first critical journey: a fresh installation is set up and is immediately
// usable, the administrator signs out and back in, and provisions a second
// Organization (ADR 0012, ADR 0013, ADR 0018).
test('first run: setup lands on a provisioned Organization, then sign in and add another', async ({ page }) => {
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

  // The installation Organization is named Default until it is renamed (ADR 0022).
  await page.getByRole('textbox', { name: 'Display name' }).fill('My Services')
  await page.getByRole('button', { name: 'Rename', exact: true }).click()
  await expect(page.getByText('Organization renamed.')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'My Services' })).toBeVisible()
  // The slug is immutable, so it still reads default.
  await expect(page.getByText('default', { exact: true })).toBeVisible()

  // The default Project can immediately hold a fully configured HTTP Monitor.
  await page.getByLabel('Monitor name').fill('ProbeHive API')
  await page.getByLabel('Target URL').fill('http://127.0.0.1:5080/readyz')
  await page.getByRole('button', { name: 'Create and activate' }).click()
  await expect(page.getByText('ProbeHive API is active.')).toBeVisible()
  await expect(page.getByRole('row', { name: /ProbeHive API Active/ })).toBeVisible()

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
