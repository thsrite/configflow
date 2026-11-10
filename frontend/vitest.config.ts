import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
 plugins: [vue()],
 resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
 test: {
  environment: 'jsdom', include: ['tests/*.test.ts'], setupFiles: ['tests/setup/dom-fastpaths.ts', 'tests/setup/panel-cache.ts'], pool: 'forks', maxWorkers: 4, minWorkers: 1,
  // Node 25+ exposes a built-in localStorage that is undefined without --localstorage-file
  // and shadows jsdom's Storage; disable it so tests use the browser implementation.
  poolOptions: { forks: { execArgv: Number(process.versions.node.split('.')[0]) >= 25 ? ['--no-experimental-webstorage'] : [] } }
 }
})
