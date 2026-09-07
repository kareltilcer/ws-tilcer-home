import { test, expect, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

// These run against the live Go backend (dev auth bypass) via the Vite proxy,
// both started by playwright.config.ts.

async function gotoTab(page: Page, name: string) {
  await page.getByRole('link', { name, exact: true }).click()
}

test('landing is Nástěnka; nav reaches the seeded board', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Nástěnka' })).toBeVisible()

  await gotoTab(page, 'Úkoly')
  await expect(page.getByRole('heading', { name: 'Zásobník' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Právě dělám' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Hotovo' })).toBeVisible()
})

test('create card → move to Právě dělám → appears on Nástěnka → hold-to-complete removes it', async ({ page }) => {
  const title = `Test ${Date.now()}`
  await page.goto('/ukoly')

  const backlog = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Zásobník' }) })
  await backlog.getByRole('button', { name: 'Přidat kartu' }).click()
  await backlog.getByPlaceholder('Název karty…').fill(title)
  await backlog.getByRole('button', { name: 'Přidat', exact: true }).click()
  await expect(page.getByText(title)).toBeVisible()

  // Move it into "Právě dělám" via the primary control (fresh DB ⇒ one card).
  await page.getByRole('button', { name: 'Přesunout do…' }).first().click()
  await page.getByRole('button', { name: 'Právě dělám', exact: true }).click()

  // It shows on the dashboard.
  await gotoTab(page, 'Nástěnka')
  await expect(page.getByText(title)).toBeVisible()

  // Press-and-hold the done control for >2s → the task completes and disappears.
  // The done control is the only button named "Dokončit" (the row body button
  // is named after the card title).
  const done = page.getByRole('button', { name: /Dokončit/ })
  const box = await done.boundingBox()
  expect(box).not.toBeNull()
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2)
  await page.mouse.down()
  await page.waitForTimeout(2300)
  await page.mouse.up()

  await expect(page.getByText(title)).toHaveCount(0)
})

test('create an event in Okno → appears in the month list', async ({ page }) => {
  const title = `Událost ${Date.now()}`
  await page.goto('/okno')
  await page.getByRole('button', { name: 'Nová událost' }).click()
  await page.getByPlaceholder('Např. Zaplatit plyn').fill(title)
  // Pick a date inside the default forward window.
  const now = new Date()
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  await page.locator('input[type="date"]').first().fill(`${y}-${m}-15`)
  await page.getByRole('button', { name: 'Vytvořit' }).click()

  await expect(page.getByText(title)).toBeVisible()
})

test('a11y: no serious/critical axe violations (both themes × 375/1440)', async ({ page }) => {
  // 2 themes × 2 viewports × 4 paths = 16 loads, each with a reload and a full
  // axe pass, in ONE test. Measured at ~39s against the config's 30s per-test
  // budget, so the fourth path is what pushed it over. Raised here rather than
  // globally, with room for a slower machine, so a genuinely hung page in any
  // other test still fails fast.
  test.setTimeout(150_000)

  for (const theme of ['dark', 'light'] as const) {
    for (const vp of [
      { width: 375, height: 800 },
      { width: 1440, height: 900 },
    ]) {
      await page.setViewportSize(vp)
      // '/' (dashboard), '/okno' (a primary accent button + form controls),
      // '/nastaveni' — the screen v10.2 moved sign-out onto, and the one that now
      // carries the app's only destructive-looking control (D273) — and
      // '/administrace', which lands on its default Rozeslat tab: with nobody
      // subscribed in a fresh DB, RecipientEcho draws the 12px text-warn line that
      // caught the light theme's --warn at 4.08:1 on white. The dev-bypass actor
      // is an admin, so the route renders the page, not RequireAdmin's refusal.
      for (const path of ['/', '/okno', '/nastaveni', '/administrace']) {
        await page.goto(path)
        await page.evaluate((t) => localStorage.setItem('home-theme', t), theme)
        await page.reload()
        await page.waitForLoadState('networkidle')

        const results = await new AxeBuilder({ page }).analyze()
        const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
        expect(
          serious,
          `axe violations (${theme}, ${vp.width}px, ${path}): ${serious
            .map((v) => `${v.id} [${v.nodes.length}]`)
            .join(', ')}`,
        ).toEqual([])
      }
    }
  }
})
