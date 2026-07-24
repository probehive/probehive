import { expect, test } from '@playwright/test'

const administratorEmail = 'admin@example.test'
const administratorPassword = 'a-long-admin-password'

// The first critical journey: a fresh installation is set up, the administrator
// signs out and back in, and creates the first Organization (ADR 0012, ADR 0013).
test('first run: setup, sign in, and create the first organization', async ({ page }) => {
  // A fresh installation routes every visitor to first-administrator setup.
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByRole('heading', { name: 'Set up ProbeHive' })).toBeVisible()

  await page.getByLabel('Email').fill(administratorEmail)
  await page.getByLabel('Display name').fill('First Administrator')
  await page.getByLabel('Password').fill(administratorPassword)
  await page.getByRole('button', { name: 'Create administrator' }).click()

  // Setup signs the administrator in and lands on the app.
  await expect(page.getByRole('heading', { name: 'Create an Organization' })).toBeVisible()
  await expect(page.getByText(administratorEmail)).toBeVisible()

  // Sign out to exercise the login journey with the created credentials.
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()

  await page.getByLabel('Email').fill(administratorEmail)
  await page.getByLabel('Password').fill(administratorPassword)
  await page.getByRole('button', { name: 'Sign in' }).click()

  // Create the first Organization and follow the link to its details.
  await expect(page.getByRole('heading', { name: 'Create an Organization' })).toBeVisible()
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
