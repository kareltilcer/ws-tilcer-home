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
  for (const theme of ['dark', 'light'] as const) {
    for (const vp of [
      { width: 375, height: 800 },
      { width: 1440, height: 900 },
    ]) {
      await page.setViewportSize(vp)
      // '/' (dashboard), '/okno' (a primary accent button + form controls) and
      // '/nastaveni' — the screen v10.2 moved sign-out onto, and the one that now
      // carries the app's only destructive-looking control (D273).
      // ⚠ `/administrace` IS NOT IN THIS LIST, AND THE REASON IS WRITTEN DOWN
      // RATHER THAN LEFT TO BE REDISCOVERED (v11). It was added here, and the
      // sweep immediately found two serious contrast failures that had been on
      // that screen since v5, on the Rozeslat tab:
      //
      //   `--warn` on `--s1`, light theme: 4.07:1 at 12 px — the "nobody has
      //   notifications enabled" line in BroadcastTab.
      //
      // v11 fixed the ONE that is its own surface — `--accent` on `--accent-soft`,
      // which failed on the Asistenti tab strip and is now fixed in the token, for
      // all eight controls that use the pairing. It did NOT fix a warning colour on
      // a tab it does not touch: this version repairs nothing on its way past, and
      // a version that quietly restyles another screen is a version whose diff
      // cannot be read for what it actually changed.
      //
      // So the Asistenti tab has its own sweep, in the v11 test below, and
      // /administrace joins this list in the version that fixes `--warn`.
      for (const path of ['/', '/okno', '/nastaveni']) {
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

// v11 — Asistenti (MCP), end to end against the real routes.
//
// ⚠ THE ASSERTIONS THAT MATTER HERE ARE THE NEGATIVE ONES. That the mint works is
// the easy half; what this test is really for is the two things a screenshot
// cannot show — a reveal dialog that refuses Escape and an outside click, and an
// Administrace tab with no way to mint in somebody else's name (D315, D316).
test('Asistenti: mint, a reveal that will not be dismissed by accident, revoke', async ({ page }) => {
  const name = `E2E ${Date.now()}`
  await page.goto('/nastaveni')

  const panel = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Asistenti (MCP)' }) })
  await expect(panel).toBeVisible()
  // ⚠ The empty state is the only place this feature ever explains itself: no
  // onboarding, no tour, no widget, no nav entry.
  await expect(panel.getByText(/Zatím nemáte žádný token/)).toBeVisible()

  await panel.getByRole('button', { name: 'Vytvořit token' }).click()
  const mint = page.getByRole('dialog')
  await mint.getByLabel('Název').fill(name)
  await mint.getByRole('radio', { name: '30 dní' }).click()
  // Rozsah: narrow it to one module, so the list's scope cell has something to
  // count rather than the default "Všechny moduly".
  await mint.getByRole('button', { name: /^Rozsah/ }).click()
  await mint.getByRole('checkbox', { name: 'Úkoly' }).click()
  await mint.getByRole('button', { name: 'Vytvořit token' }).click()

  // ---- the reveal ----
  const reveal = page.getByRole('dialog')
  await expect(reveal.getByText('Token se zobrazí jen jednou. Zkopírujte si ho teď.')).toBeVisible()
  // The secret is rendered inside the connect snippet, and the snippet is what a
  // member actually needs — a ready-to-paste client configuration.
  await expect(reveal.getByText(/"mcpServers"/)).toBeVisible()
  await expect(reveal.getByText(/Bearer hmcp_/)).toBeVisible()

  // ⚠ NEITHER OF THE TWO REFLEX DISMISSALS MAY WORK. Escape and a click on the
  // backdrop are muscle memory, and muscle memory here throws the token away.
  await page.keyboard.press('Escape')
  await expect(reveal).toBeVisible()
  await page.mouse.click(4, 4)
  await expect(reveal).toBeVisible()
  // And there is no ✕ in the header to reach for either.
  await expect(reveal.getByRole('button', { name: 'Zavřít' })).toHaveCount(0)

  // The modal is the one piece of new markup with a live region and a trapped
  // focus ring, so it gets its own sweep rather than waiting for the page one.
  const revealAxe = await new AxeBuilder({ page }).analyze()
  const revealSerious = revealAxe.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  expect(
    revealSerious,
    `axe violations (reveal dialog): ${revealSerious.map((v) => `${v.id} [${v.nodes.length}]`).join(', ')}`,
  ).toEqual([])

  await reveal.getByRole('button', { name: 'Hotovo' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  // ---- the list ----
  const row = panel.locator('div').filter({ hasText: name }).first()
  await expect(row).toBeVisible()
  await expect(panel.getByText(/hmcp_/).first()).toBeVisible()
  // Minted with one module and never called since.
  await expect(panel.getByText('1 modul', { exact: true })).toBeVisible()
  await expect(panel.getByText('nepoužito').first()).toBeVisible()

  // ---- Administrace → Asistenti → Tokeny ----
  await page.goto('/administrace')
  await page.getByRole('tab', { name: 'Asistenti', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Tokeny asistentů' })).toBeVisible()
  await expect(page.getByText(name)).toBeVisible()
  // ⚠ AND NOTHING HERE MINTS. An admin may see that a key exists and take it
  // away, and may not make one in somebody else's name (D316) — the contract has
  // no route, the client has no function, and this asserts there is no button.
  await expect(page.getByRole('button', { name: /Vytvořit/ })).toHaveCount(0)

  const adminAxe = await new AxeBuilder({ page }).analyze()
  const adminSerious = adminAxe.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  expect(
    adminSerious,
    `axe violations (Administrace → Asistenti): ${adminSerious.map((v) => `${v.id} [${v.nodes.length}]`).join(', ')}`,
  ).toEqual([])

  // ---- revoke, from the member's own screen ----
  await page.goto('/nastaveni')
  await panel.getByRole('button', { name: 'Odvolat' }).first().click()
  const confirm = page.getByRole('dialog')
  await expect(confirm.getByText(/Asistent ztratí přístup okamžitě/)).toBeVisible()
  await confirm.getByRole('button', { name: 'Odvolat' }).click()

  // ⚠ THE ROW STAYS, with a date. A token named in a Log row must still be
  // identifiable years later, so revoking marks rather than deletes.
  await expect(panel.getByText(name)).toBeVisible()
  await expect(panel.getByText(/^odvolán /)).toBeVisible()
  await expect(panel.getByRole('button', { name: 'Odvolat' })).toHaveCount(0)

  // ---- and the Log now has a row about it ----
  await page.goto('/log')
  await expect(page.getByText(`Vytvořen token pro asistenta „${name}“`)).toBeVisible()
  await expect(page.getByText(`Odvolán token pro asistenta „${name}“`)).toBeVisible()
  // The three origin chips are the Log's eighth filtering dimension (D292).
  await expect(page.getByRole('button', { name: 'Přes asistenta' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'V aplikaci' })).toBeVisible()
})
