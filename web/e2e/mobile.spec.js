import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const SHOTS = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'docs', 'screenshots')

// Mobile-only: the landing and creation flow must be usable at a phone
// viewport (the config's `mobile` project uses iPhone 13 / 390×844).
test('mobile landing is usable and creates a room', async ({ page }, info) => {
  test.skip(info.project.name !== 'mobile', 'mobile only')
  const errors = []
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
  page.on('pageerror', (e) => errors.push(String(e)))

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Fauxlands' })).toBeVisible()
  await page.screenshot({ path: resolve(SHOTS, 'mobile-landing.png'), fullPage: true })

  await page.getByPlaceholder('e.g. Robin').fill('Mobile')
  const createBtn = page.getByRole('button', { name: 'Create room' })
  await expect(createBtn).toBeVisible()
  await createBtn.click()
  await expect(page.locator('.room-code')).toBeVisible()
  await page.screenshot({ path: resolve(SHOTS, 'mobile-lobby.png'), fullPage: true })
  expect(errors).toEqual([])
})
