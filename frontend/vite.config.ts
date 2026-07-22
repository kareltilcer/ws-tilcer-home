import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), './src')

// The SPA is served same-origin with the API in production. In dev, proxy /api
// and /ws to the Go backend (default :8080) so the fetch wrapper and websocket
// work without CORS.
export default defineConfig({
  plugins: [react(), tailwindcss()],
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
