// Route path constants — the single source of truth for the app's URL paths.
// Referenced by the router (App.tsx), the nav (AppShell.tsx) and the live-sync
// toast routing (api/ws.ts) so a path is renamed in exactly one place and the
// type→route mapping in ws.ts stays compile-time linked to the real routes.
export const routes = {
  nastenka: '/',
  ukoly: '/ukoly',
  okno: '/okno',
  poznamky: '/poznamky',
  log: '/log',
} as const
