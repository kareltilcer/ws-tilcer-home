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
} as const

// There is deliberately no permalink helper here: /d/{id} (D42) is minted by the
// backend and handed to the client as `urls.permalink` on the document detail, so
// that HOME_DOCS_PUBLIC_BASE_URL can absolutise it. Building it client-side would
// bypass that setting. The route itself is registered in App.tsx.
