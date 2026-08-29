import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { queryClient } from '@/app/queryClient'
import { routes } from '@/app/routes'
import { ThemeProvider } from '@/theme/theme'
import { AuthProvider, useAuth } from '@/app/auth'
import { AppShell } from '@/app/AppShell'
import { AccessDenied } from '@/components/common/states'
import { NastenkaPage } from '@/modules/dashboard/NastenkaPage'
import { UkolyPage } from '@/modules/todo/UkolyPage'
import { OknoPage } from '@/modules/events/OknoPage'
import { PoznamkyPage } from '@/modules/notes/PoznamkyPage'
import { DokumentyPage } from '@/modules/documents/DokumentyPage'
import { DocumentPermalinkPage } from '@/modules/documents/DocumentPermalinkPage'
import { FinancePage } from '@/modules/finance/FinancePage'
import { GardenPage } from '@/modules/garden/GardenPage'
import { ElectricityPage } from '@/modules/electricity/ElectricityPage'
import { ChatPage } from '@/modules/chat/ChatPage'
import { LogPage } from '@/modules/logging/LogPage'
import { AdministracePage } from '@/modules/admin/AdministracePage'
import { NastaveniPage } from '@/platform/settings/NastaveniPage'
import { OnlineProvider } from '@/platform/pwa/offline'

// RequireAdmin guards the Log route at the route level (not just by hiding the
// nav item): a non-admin who deep-links to /log gets the refusal screen.
function RequireAdmin({ children }: { children: ReactNode }) {
  const { isAdmin } = useAuth()
  return isAdmin ? <>{children}</> : <AccessDenied />
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        {/* Connectivity is app-wide state: the offline banner and every disabled
            write control read from here (D71). */}
        <OnlineProvider>
        <AuthProvider>
          <BrowserRouter>
            <Routes>
              <Route element={<AppShell />}>
                <Route path={routes.nastenka} element={<NastenkaPage />} />
                <Route path={routes.ukoly} element={<UkolyPage />} />
                <Route path={routes.okno} element={<OknoPage />} />
                {/* Slug-path routes: /poznamky and /poznamky/<folder>/…/<slug> */}
                <Route path={`${routes.poznamky}/*`} element={<PoznamkyPage />} />
                {/* Dokumenty: the same slug-path navigation, plus /d/{id} — the
                    PERMANENT id-based link, which survives renames and moves (D42). */}
                <Route path={`${routes.dokumenty}/*`} element={<DokumentyPage />} />
                <Route path="/d/:id" element={<DocumentPermalinkPage />} />
                {/* v6: Finance is an ordinary all-roles route — no RequireAdmin.
                    A reader sees the whole module and can change none of it. */}
                <Route path={routes.finance} element={<FinancePage />} />
                {/* v7: Zahrada is the first module with SUB-ROUTES — eight of
                    them, matched by its own router under this prefix. Also an
                    ordinary all-roles route: a reader sees the whole module and
                    can change none of it. */}
                <Route path={`${routes.zahrada}/*`} element={<GardenPage />} />
                {/* v8: Elektřina — four sub-routes under one prefix, the same
                    shape as Zahrada. An ordinary all-roles route with no admin
                    gate anywhere in the module (D151): a reader sees every
                    number and can change nothing. */}
                <Route path={`${routes.elektrina}/*`} element={<ElectricityPage />} />
                {/* v10: Chat — the list and one thread under one prefix, plus the
                    clean-up screen at /chat/uklid.
                    ⚠ IT IS THE ONE ROUTE THE SHELL DOES NOT PAD. The list and the
                    thread are panes sized to the viewport rather than a document
                    that scrolls, so `<main>` hands them its content box whole —
                    isFullBleedRoute in app/routes.ts is where that is decided, and
                    /chat/uklid is deliberately outside it.
                    ⚠ And there is NO RequireWrite here or anywhere in the module: a
                    `reader` writes in chat (D222), which is a first for Home. The
                    gate is membership, enforced in SQL. */}
                <Route path={`${routes.chat}/*`} element={<ChatPage />} />
                <Route
                  path={routes.log}
                  element={
                    <RequireAdmin>
                      <LogPage />
                    </RequireAdmin>
                  }
                />
                {/* v5: Administrace is admin-only at the ROUTE level too — a
                    non-admin deep-linking here gets the refusal screen, not a
                    stripped-down view. Nastavení is for everyone. */}
                <Route
                  path={routes.administrace}
                  element={
                    <RequireAdmin>
                      <AdministracePage />
                    </RequireAdmin>
                  }
                />
                <Route path={routes.nastaveni} element={<NastaveniPage />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Route>
            </Routes>
          </BrowserRouter>
        </AuthProvider>
        </OnlineProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
