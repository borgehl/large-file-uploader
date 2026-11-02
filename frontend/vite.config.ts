import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  worker: { format: 'es' },
  test: {
    globals: true,
    environment: 'node',
    // e2e files share one live stack (and one restarts it), so files must not run concurrently
    fileParallelism: false,
  },
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: false },
      '/files': { target: 'http://localhost:8080', changeOrigin: false },
    },
  },
})
