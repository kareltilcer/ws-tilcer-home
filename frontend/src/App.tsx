import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { queryClient } from '@/app/queryClient'
import { routes } from '@/app/routes'
import { ThemeProvider } from '@/theme/theme'
import { AuthProvider, useAuth } from '@/app/auth'
import { AppShell } from '@/app/AppShell'
import { AccessDenied } from '@/components/common/states'
import { NastenkaPage } from '@/routes/nastenka/NastenkaPage'
import { UkolyPage } from '@/routes/ukoly/UkolyPage'
import { OknoPage } from '@/routes/okno/OknoPage'
import { PoznamkyPage } from '@/routes/poznamky/PoznamkyPage'
import { DokumentyPage } from '@/routes/dokumenty/DokumentyPage'
import { DocumentPermalinkPage } from '@/routes/dokumenty/DocumentPermalinkPage'
import { FinancePage } from '@/routes/finance/FinancePage'
import { LogPage } from '@/routes/log/LogPage'
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
