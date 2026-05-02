import { defineConfig } from 'vite'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      '^/(api|auth|healthz)': 'http://127.0.0.1:8080',
    },
  },
  build: {
    outDir: resolve(__dirname, '../internal/webui/static'),
    emptyOutDir: false,
  },
})
