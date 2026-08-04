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
import { LogPage } from '@/routes/log/LogPage'

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
        <AuthProvider>
          <BrowserRouter>
            <Routes>
              <Route element={<AppShell />}>
                <Route path={routes.nastenka} element={<NastenkaPage />} />
                <Route path={routes.ukoly} element={<UkolyPage />} />
                <Route path={routes.okno} element={<OknoPage />} />
                {/* Slug-path routes: /poznamky and /poznamky/<folder>/…/<slug> */}
                <Route path={`${routes.poznamky}/*`} element={<PoznamkyPage />} />
                <Route
                  path={routes.log}
                  element={
                    <RequireAdmin>
                      <LogPage />
                    </RequireAdmin>
                  }
                />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Route>
            </Routes>
          </BrowserRouter>
        </AuthProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
