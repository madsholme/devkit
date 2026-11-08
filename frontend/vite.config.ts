import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { uiDependencies } from '@madsholme/ui/vite'
import { themeBoot } from '@madsholme/themes/vite'

export default defineConfig({
  plugins: [
    uiDependencies(),
    tailwindcss(),
    vue(),
    themeBoot({ storageKey: 'devkit.swift-theme' }),
  ],
  build: { outDir: '../web/dist', emptyOutDir: true },
  // Same-origin API forwarding for local development; the production binary serves both.
  server: { proxy: { '/api': 'http://127.0.0.1:3780' } },
})
