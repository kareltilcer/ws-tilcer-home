// Route path constants — the single source of truth for the app's URL paths.
// Referenced by the router (App.tsx), the nav (AppShell.tsx) and the live-sync
// toast routing (api/ws.ts) so a path is renamed in exactly one place and the
// type→route mapping in ws.ts stays compile-time linked to the real routes.
export const routes = {
  nastenka: '/',
  ukoly: '/ukoly',
  okno: '/okno',
  poznamky: '/poznamky',
  dokumenty: '/dokumenty',
  log: '/log',
  // v5: Administrace is admin-only and lives in the "Více" overflow beside Log;
  // Nastavení is for everyone (it holds the per-device notification panel).
  administrace: '/administrace',
  nastaveni: '/nastaveni',
  // v6: Finance is an ordinary all-roles destination in the "Více" overflow — a
  // once-a-month screen does not earn one of the four thumb tabs (D84).
  finance: '/finance',
  // v7: Zahrada joins the same overflow for everyone, and is the first module
  // with SUB-PAGES — eight of them behind one nav entry, with an in-page tab
  // strip. The four thumb tabs stay untouched; the sub-routes hang off this
  // prefix (/zahrada/plodiny, /zahrada/plan/2027, …).
  zahrada: '/zahrada',
} as const

// There is deliberately no permalink helper here: /d/{id} (D42) is minted by the
// backend and handed to the client as `urls.permalink` on the document detail, so
// that HOME_DOCS_PUBLIC_BASE_URL can absolutise it. Building it client-side would
// bypass that setting. The route itself is registered in App.tsx.
