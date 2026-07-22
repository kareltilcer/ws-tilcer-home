import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), './src')

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': srcDir } },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    // Only unit tests under src/ — the e2e/ Playwright specs run via `playwright test`.
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
