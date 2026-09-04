import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Self-hosted fonts (same-origin; family names match the design tokens
// 'Hanken Grotesk' / 'IBM Plex Mono'). The weight files are latin-only, so the
// latin-ext subset (Czech diacritics: ě š č ř ž ů ...) is imported explicitly.
import '@fontsource/hanken-grotesk/400.css'
import '@fontsource/hanken-grotesk/500.css'
import '@fontsource/hanken-grotesk/600.css'
import '@fontsource/hanken-grotesk/700.css'
import '@fontsource/hanken-grotesk/800.css'
import '@fontsource/hanken-grotesk/latin-ext-400.css'
import '@fontsource/hanken-grotesk/latin-ext-500.css'
import '@fontsource/hanken-grotesk/latin-ext-600.css'
import '@fontsource/hanken-grotesk/latin-ext-700.css'
import '@fontsource/hanken-grotesk/latin-ext-800.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import '@fontsource/ibm-plex-mono/latin-ext-400.css'
import '@fontsource/ibm-plex-mono/latin-ext-500.css'
import '@fontsource/ibm-plex-mono/latin-ext-600.css'
import './theme/globals.css'
import App from './App.tsx'
import { registerServiceWorker } from '@/platform/pwa/register'
import { initCrashReporting } from '@/platform/status/report'

// BEFORE the first render, and it has to be: a crash while the app is still
// booting is the one nobody can report by hand, and it is also the one that
// leaves a member on the login screen with nothing to describe. Off entirely in
// a build with no VITE_STATUS_INGEST_* args.
initCrashReporting()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// One service worker for both push and the offline app shell (D63). Registered
// after the app mounts so it never delays first paint.
registerServiceWorker()
