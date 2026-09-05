import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), './src')

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': srcDir } },
  // ⚠ THE TESTS MAY READ THE REPO, NOT JUST `frontend/`. version.test.ts asserts
  // APP_VERSION against the newest heading in `handoff/v10/CHANGELOG.md`, which is one
  // directory above Vite's root and is therefore refused ("Denied ID") by the default
  // fs allow-list. It is widened HERE and not in `vite.config.ts` on purpose: that file
  // configures the dev server Karel actually browses, and nothing the browser loads has
  // any business reading the handoff folder.
  server: { fs: { allow: [path.resolve(srcDir, '../..')] } },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    // Only unit tests under src/ — the e2e/ Playwright specs run via `playwright test`.
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
