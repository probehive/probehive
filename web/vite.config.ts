/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { configDefaults } from 'vitest/config'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    // Development-only convenience; production uses a same-origin gateway (ADR 0010).
    // The browser Host header must be preserved (no changeOrigin) so the API's
    // same-origin validation of Origin/Referer accepts proxied unsafe requests.
    proxy: {
      '/api': { target: 'http://localhost:5080' },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // Playwright owns e2e/; Vitest must not pick up its spec files.
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
})
