import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import pkg from './package.json'

// Tailwind 4 通过 Vite 插件接入，不生成 tailwind.config.js（DESIGN-SPEC §1）。
// base 必须是相对路径：Wails 用 file 协议加载打包产物。
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  base: './',
  // 版本号只有一个来源：frontend/package.json。侧栏用它渲染，避免写死后漂移。
  define: { __APP_VERSION__: JSON.stringify(pkg.version) },
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: { outDir: 'dist', emptyOutDir: true },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.spec.ts'],
  },
})
