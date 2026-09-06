import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const SHOTS = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'docs', 'screenshots')
const desktopOnly = ({}, info) => info.project.name !== 'desktop'

// Fail any test that logs an uncaught console error.
function trackConsole(page, errors) {
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text())
  })
  page.on('pageerror', (e) => errors.push(String(e)))
}

async function createRoom(page, name) {
  await page.goto('/')
  await page.getByPlaceholder('e.g. Robin').fill(name)
  await page.getByRole('button', { name: 'Create room' }).click()
  await expect(page.locator('.room-code')).toBeVisible()
  return (await page.locator('.room-code').innerText()).trim()
}

async function joinRoom(page, code, name) {
  await page.goto(`/join/${code}`)
  await page.getByPlaceholder('e.g. Robin').fill(name)
  await page.getByRole('button', { name: 'Join', exact: true }).click()
  await expect(page.locator('.lobby')).toBeVisible()
}

test('landing renders the brand', async ({ page }) => {
  const errors = []
  trackConsole(page, errors)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Fauxlands' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Create room' })).toBeVisible()
  if (test.info().project.name === 'desktop') {
    await page.screenshot({ path: resolve(SHOTS, 'landing.png'), fullPage: true })
  }
  expect(errors).toEqual([])
})

test('create and join reach a shared lobby', async ({ browser }, info) => {
  test.skip(desktopOnly({}, info), 'desktop only (multi-context)')
  const errors = []

  const hostCtx = await browser.newContext()
  const host = await hostCtx.newPage()
  trackConsole(host, errors)
  const code = await createRoom(host, 'Alice')
  await expect(host.locator('.lobby-player')).toHaveCount(1)

  const bobCtx = await browser.newContext()
  const bob = await bobCtx.newPage()
  trackConsole(bob, errors)
  await joinRoom(bob, code, 'Bob')

  // The host now sees two players.
  await expect(host.locator('.lobby-player')).toHaveCount(2)
  await host.screenshot({ path: resolve(SHOTS, 'lobby.png'), fullPage: true })

  await hostCtx.close()
  await bobCtx.close()
  expect(errors).toEqual([])
})

test('a full match renders the board and reaches game over', async ({ browser }, info) => {
  test.skip(desktopOnly({}, info), 'desktop only (multi-context)')
  test.setTimeout(120_000)
  const errors = []
  const ctxs = []
  const pages = []
  const names = ['Alice', 'Bob', 'Cara']

  const hostCtx = await browser.newContext()
  ctxs.push(hostCtx)
  const host = await hostCtx.newPage()
  pages.push(host)
  trackConsole(host, errors)
  const code = await createRoom(host, names[0])

  for (let i = 1; i < 3; i++) {
    const ctx = await browser.newContext()
    ctxs.push(ctx)
    const p = await ctx.newPage()
    pages.push(p)
    trackConsole(p, errors)
    await joinRoom(p, code, names[i])
  }

  // Host picks Quick (6 rounds); everyone readies; host starts.
  await host.getByText('Quick', { exact: false }).first().click()
  for (const p of pages) {
    await p.getByRole('button', { name: /Ready up/ }).click()
  }
  await expect(host.getByRole('button', { name: 'Start match' })).toBeEnabled()
  await host.getByRole('button', { name: 'Start match' }).click()

  // The board renders once the match is underway.
  await expect(host.locator('.hex-map')).toBeVisible({ timeout: 15_000 })
  await host.screenshot({ path: resolve(SHOTS, 'board.png'), fullPage: true })

  // Best-effort transient-phase screenshots (auto-hold match; recurs each round).
  await grabWhenVisible(host, host.getByText('Declarations revealed'), resolve(SHOTS, 'declaration-reveal.png'))
  await grabWhenVisible(host, host.getByText(/Round \d+ summary/), resolve(SHOTS, 'round-summary.png'))

  // The match reaches game over.
  await expect(host.locator('.results-banner')).toBeVisible({ timeout: 90_000 })
  await host.screenshot({ path: resolve(SHOTS, 'game-over.png'), fullPage: true })

  for (const c of ctxs) await c.close()
  // Auto-hold + shared draws are expected; only assert we saw no console errors.
  expect(errors).toEqual([])
})

async function grabWhenVisible(page, locator, path) {
  try {
    await locator.first().waitFor({ state: 'visible', timeout: 30_000 })
    await page.screenshot({ path, fullPage: true })
  } catch {
    // Transient phase not caught this run — non-fatal; the state is covered by
    // the Node E2E and the manual test plan.
  }
}
