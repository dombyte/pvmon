import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'
import { entryFileNames, pwa, pwaInput } from './pwa.plugin'

const backend = 'http://localhost:8080'

// Initial bundle budget (spec 14): < 250 KB gzip. Vite warns above this raw chunk size;
// ECharts is lazy-loaded, so the entry chunk stays well below.
const chunkWarningKb = 600

export default defineConfig({
  plugins: [vue(), tailwindcss(), pwa()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    proxy: {
      '/api': backend,
      '/health': backend,
      '/ws': { target: backend, ws: true },
    },
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: chunkWarningKb,
    rollupOptions: { input: pwaInput, output: { entryFileNames } },
  },
  test: { environment: 'happy-dom', include: ['src/**/*.test.ts'] },
})
