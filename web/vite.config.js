import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
  // Vitest runs the unit tests under src/ only. The Playwright browser E2E
  // specs live under e2e/ and are run by `npm run test:e2e`, never by vitest.
  test: { environment: 'node', include: ['src/**/*.test.{js,jsx}'] },
})
