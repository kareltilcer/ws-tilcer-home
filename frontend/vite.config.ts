import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), './src')

// v5 (D67/D63): Home is an installable PWA with ONE service worker that does both
// push and the app-shell cache. injectManifest (not generateSW) because the SW is
// hand-written — generateSW cannot host our push/notificationclick handlers, and
// hand-rolling the precache manifest would mean re-implementing Vite's content
// hashing. The plugin only injects `self.__WB_MANIFEST` into our own src/sw.ts.
const pwa = VitePWA({
  strategies: 'injectManifest',
  srcDir: 'src',
  filename: 'sw.ts',
  registerType: 'autoUpdate',
  injectRegister: null, // registered explicitly in src/platform/pwa/register.ts
  injectManifest: {
    // Document bytes are never cached (D73) and the SW itself must stay fresh.
    globPatterns: ['**/*.{js,css,html,woff2,svg,png,ico,webmanifest}'],
  },
  manifest: {
    name: 'Home — domácnost',
    short_name: 'Home',
    description: 'Domácí systém: úkoly, události, poznámky a dokumenty.',
    start_url: '/',
    scope: '/',
    display: 'standalone',
    // Dark, matching the app's default theme: a white splash on an OLED phone
    // at 6am is exactly the thing the dark-first design exists to avoid.
    theme_color: '#0f1115',
    background_color: '#0f1115',
    lang: 'cs',
    icons: [
      { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
      { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
      { src: '/icons/maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
    ],
  },
  devOptions: { enabled: false },
})

// The SPA is served same-origin with the API in production. In dev, proxy /api
// and /ws to the Go backend (default :8080) so the fetch wrapper and websocket
// work without CORS.
export default defineConfig({
  plugins: [react(), tailwindcss(), pwa],
  resolve: {
    alias: { '@': srcDir },
  },
  server: {
    // 127.0.0.1 (not "localhost") so the proxy hits the Go backend's IPv4 bind
    // rather than IPv6 ::1 on Windows.
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      '/ws': { target: 'ws://127.0.0.1:8080', ws: true },
    },
  },
})
