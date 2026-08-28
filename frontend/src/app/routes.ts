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
  // v8: Elektřina joins the same overflow for everyone — four sub-routes behind
  // one nav entry, reusing v7's tab-strip pattern rather than inventing a second.
  // Note that /elektrina/cenik is SINGULAR: the PRD's spelling wins over the
  // build brief's /ceniky, while the tab LABEL stays "Ceníky a poplatky".
  elektrina: '/elektrina',
  // v10: Chat. It now HAS a thumb tab, and Okno do budoucnosti moved into "Více"
  // to make room — the first demotion in this app's history (D260). Four
  // thumb-reachable tabs plus *Více* is the shape that works at 375 px and a fifth
  // makes six. `/chat/{id}` renders the list AND the thread at ≥1024.
  chat: '/chat',
  // Úklid úložiště chatu (D241). ⚠ IT IS A CHAT SUB-ROUTE, NOT AN ADMINISTRACE
  // TAB: the listing is member-scoped, so it belongs to the module whose membership
  // decides what it can show — an admin outside every conversation sees nothing here
  // and has no business on this screen (D240).
  chatUklid: '/chat/uklid',
} as const

// There is deliberately no permalink helper here: /d/{id} (D42) is minted by the
// backend and handed to the client as `urls.permalink` on the document detail, so
// that HOME_DOCS_PUBLIC_BASE_URL can absolutise it. Building it client-side would
// bypass that setting. The route itself is registered in App.tsx.

/**
 * The order the DESKTOP side nav lists its destinations in (design/v10, the side
 * nav at lines 828–842 of Home.dc.html).
 *
 * ⚠ IT IS NOT THE THUMB BAR'S ORDER, AND THE DIFFERENCE IS THE POINT. The phone bar
 * is ordered by REACH — four tabs plus *Více*, with the one entry carrying an unread
 * count in the middle slot, which is the easiest one to hit. A sidebar has no reach
 * to trade on, so the artboards order it by REASON TO LOOK: Chat leads, because it
 * is the only entry that can be waiting for you, and Okno do budoucnosti keeps the
 * fourth slot it has always had here — D260 took its THUMB TAB, not its place in a
 * list that has room for everything. Deriving this list from the two nav tables is
 * what had Chat fourth and Okno fifth.
 *
 * ⚠ EVERY NAV ENTRY HAS TO APPEAR HERE. AppShell sorts by this list and appends
 * whatever it does not name, so a module added to the nav and forgotten here lands
 * at the bottom of the sidebar rather than vanishing from it — and nav.test.ts
 * fails, which is the intended way to find that out.
 */
export const DESKTOP_NAV_ORDER: readonly string[] = [
  routes.chat,
  routes.nastenka,
  routes.ukoly,
  routes.okno,
  routes.poznamky,
  routes.dokumenty,
  routes.finance,
  routes.zahrada,
  routes.elektrina,
  routes.log,
  routes.administrace,
  routes.nastaveni,
]

/** What AppShell hands down through its `<Outlet>`: the layout it already decided. */
export interface ShellLayout {
  /** True when `<main>` gave this route its content box unpadded (isFullBleedRoute). */
  fullBleed: boolean
}

/**
 * Whether a path owns the shell's whole content box rather than sitting inside its
 * padding (design/v10, desktop and 375 px frames).
 *
 * ⚠ CHAT IS THE ONLY ONE, AND IT IS NOT A STYLE PREFERENCE. Every other module is a
 * document that scrolls: the shell's `px-4 py-5 pb-24 md:px-8 md:py-8 md:pb-8` is
 * its margin and the page grows as tall as it needs. The chat list and thread are
 * PANES — each one a fixed-height flex column with its own scroll box — so the
 * padding is not a margin around them, it is height subtracted from an element that
 * has already been sized to the full viewport. Below 768 that arithmetic ran twice and
 * the page gained 49 px it could scroll and no content to put there, with the
 * composer sitting above a dead strip; the artboards draw the two panes flush to
 * the shell on both frames, with no card frame around them.
 *
 * ⚠ `/chat/uklid` IS NOT FULL-BLEED, and that is the design too (D241): the
 * clean-up screen is an ordinary scrolling document, like the ten modules either
 * side of it. Excluding it is safe for the same reason the route tree is:
 * `uklid` is a reserved word under `/chat` and a conversation is addressed by
 * UUIDv7 (D220 — no slugs anywhere in the module), so no room can ever claim it.
 *
 * ⚠ IT HAS TO MATCH EXACTLY WHAT THE ROUTER MATCHES, which is looser than an `===`
 * on a route constant in three ways. Anything the router sends to a chat screen and
 * this function says no to gets the shell's padding on top of a viewport-sized pane,
 * which is the bug this whole predicate exists to remove.
 */
export function isFullBleedRoute(pathname: string): boolean {
  // 1. React Router matches route paths CASE-INSENSITIVELY unless a route opts in
  //    (`caseSensitive`), and nothing in App.tsx does — so `/CHAT` really does
  //    render the module, and a lowercase-only comparison would pad it.
  // 2. It treats `/chat/uklid/` as the same location as `/chat/uklid`, so one stray
  //    trailing slash must not unpad the one chat screen that needs the padding.
  const lower = pathname.toLowerCase()
  const path = lower.length > 1 ? lower.replace(/\/+$/, '') : lower
  // 3. The clean-up screen is excluded WITH ANYTHING UNDER IT. `uklid` has no
  //    sub-routes today, so this is about the day it grows one: an exact-match
  //    exclusion would quietly hand that sub-page the full-bleed box and put its
  //    heading against the shell's edge.
  if (path === routes.chatUklid || path.startsWith(routes.chatUklid + '/')) return false
  return path === routes.chat || path.startsWith(routes.chat + '/')
}
