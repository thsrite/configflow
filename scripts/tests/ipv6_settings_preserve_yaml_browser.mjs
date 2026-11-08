#!/usr/bin/env node
// IPv6 mode in the real app writes back only the IPv6 lines of the base YAML.
// Test server only, from the task worktree:
//   cf-test typecheck   # syncs and links frontend/node_modules
//   rt --no-sync --back .ci-results/ipv6-settings-preserve-yaml -- node scripts/tests/ipv6_settings_preserve_yaml_browser.mjs
// Real Vue frontend and Flask API on a temporary data directory (no login, no Agent, nothing published).
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run this on the test server')
const root = process.cwd()
const evidence = resolve(root, '.ci-results/ipv6-settings-preserve-yaml')
const apiPort = Number(process.env.CF_API_PORT || 15941)
const webPort = Number(process.env.CF_WEB_PORT || 15942)
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const yamlModule = await import(pathToFileURL(resolve(root, 'frontend/node_modules/yaml/dist/index.js')))
const parse = yamlModule.parse ?? yamlModule.default.parse
const temporary = await mkdtemp(join(tmpdir(), 'configflow-ipv6-preserve-'))
const report = { steps: [], browserErrors: [] }
const base = `http://127.0.0.1:${webPort}`
let backend, vite, browser, backendLog = ''

const step = (name, detail = {}) => { report.steps.push({ name, ...detail }); console.log('✓', name) }
async function waitFor(url) {
  for (let attempt = 0; attempt < 150; attempt++) {
    try { if ((await fetch(url)).ok) return } catch {}
    if (backend?.exitCode !== null) throw new Error(`Backend exited: ${backendLog}`)
    await new Promise(done => setTimeout(done, 200))
  }
  throw new Error(`Timed out waiting for ${url}\n${backendLog}`)
}
async function api(method, path, body) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/api${path}`, {
    method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })
  return { status: response.status, data: await response.json() }
}

/** Lines removed from / added to `before`, by longest common subsequence. */
function lineDiff(before, after) {
  const a = before.split('\n')
  const b = after.split('\n')
  const table = Array.from({ length: a.length + 1 }, () => new Array(b.length + 1).fill(0))
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1])
  }
  const removed = []
  const added = []
  let i = 0
  let j = 0
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) { i++; j++ }
    else if (j < b.length && (i === a.length || table[i][j + 1] >= table[i + 1][j])) added.push(b[j++])
    else removed.push(a[i++])
  }
  return { removed, added }
}
// Read as the server's PyYAML does (YAML 1.1).
const server = text => parse(text, { version: '1.1', merge: true })

// The editor renders only visible lines; read the whole document from CodeMirror.
const editorText = tab => tab.$eval('.cm-content', element => element.cmView?.view?.state.doc.toString()
  ?? [...element.querySelectorAll('.cm-line')].map(line => line.textContent).join('\n'))
// The mode is a cell of the IPv6 card; its choices open in a menu.
const choose = async (tab, label) => {
  await tab.locator('[data-ipv6-mode-trigger]').click()
  await tab.getByRole('radio', { name: label, exact: true }).click()
  await tab.waitForTimeout(400)
}
async function save(tab) {
  await tab.locator('[data-settings-publish-bar]').getByRole('button', { name: '保存草稿' }).click()
  await tab.getByText('基础配置草稿已保存').first().waitFor()
  await tab.waitForTimeout(300)
  return (await api('GET', '/custom-config/mihomo')).data.config
}

try {
  await mkdir(evidence, { recursive: true })
  const launcher = `from pathlib import Path
import json, os
from backend.common.config import get_default_config, DATA_DIR
from backend.common.setup_flow import initial_mihomo_base
config = get_default_config()
# A new installation's default profile starts from the initial template.
config.setdefault('mihomo', {})['custom_config'] = initial_mihomo_base()
Path(DATA_DIR, 'config.json').write_text(json.dumps(config), encoding='utf-8')
from backend.app import app
app.run(host='127.0.0.1', port=int(os.environ['CF_API_PORT']), debug=False, use_reloader=False)
`
  backend = spawn(python, ['-c', launcher], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, DATA_DIR: temporary, LOG_FILE: join(temporary, 'api.log'), LOG_LEVEL: 'WARNING', ADMIN_USERNAME: '', ADMIN_PASSWORD: '',
      JWT_SECRET_KEY: 'isolated-browser-test-only', CF_API_PORT: String(apiPort) } })
  backend.stdout.on('data', chunk => { backendLog += chunk })
  backend.stderr.on('data', chunk => { backendLog += chunk })
  await waitFor(`http://127.0.0.1:${apiPort}/api/auth/status`)
  const { createServer } = await import(pathToFileURL(resolve(root, 'frontend/node_modules/vite/dist/node/index.js')))
  vite = await createServer({ root: resolve(root, 'frontend'), configFile: resolve(root, 'frontend/vite.config.mts'), logLevel: 'error',
    server: { host: '127.0.0.1', port: webPort, strictPort: true, proxy: { '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true } } } })
  await vite.listen()
  const { chromium } = await import(pathToFileURL(playwright))
  browser = await chromium.launch({ headless: true })

  const original = (await api('GET', '/custom-config/mihomo')).data.config
  assert.match(original, /^ipv6: false$/m)
  assert.match(original, /ports: \[80, 8080-8880\]/)

  const context = await browser.newContext({ viewport: { width: 1280, height: 1000 } })
  const tab = await context.newPage()
  tab.on('pageerror', error => report.browserErrors.push(error.message))
  await tab.goto(`${base}/generate`)
  await tab.getByRole('button', { name: '基础配置' }).first().click()
  await tab.locator('[data-ipv6-mode-trigger]').waitFor({ timeout: 30000 })
  await tab.waitForTimeout(400)
  const shown = await editorText(tab)
  assert.equal(shown.replace(/\n$/, ''), original.replace(/\n$/, ''), 'the editor shows the saved template')

  // 客户端也走 IPv6: two switches and the Fake-IP range, nothing else.
  await choose(tab, '客户端也走 IPv6')
  const inEditor = await editorText(tab)
  let diff = lineDiff(original, inEditor)
  assert.deepEqual(diff, { removed: ['ipv6: false', '  ipv6: false'], added: ['ipv6: true', '  ipv6: true', '  fake-ip-range6: fc00::1/64'] })
  await tab.locator('[data-ipv6-mode]').scrollIntoViewIfNeeded()
  await tab.screenshot({ path: join(evidence, 'clients.png'), animations: 'disabled' })
  const clients = await save(tab)
  assert.deepEqual(lineDiff(original, clients), diff)
  const expected = server(original)
  expected.ipv6 = true
  expected.dns = { ...expected.dns, ipv6: true, 'fake-ip-range6': 'fc00::1/64' }
  assert.deepEqual(server(clients), expected)
  step('客户端也走 IPv6 changes only the IPv6 lines', diff)

  // Mihomo 用 IPv6，客户端走 IPv4: only dns.ipv6 goes back.
  await choose(tab, 'Mihomo 用 IPv6，客户端走 IPv4')
  diff = lineDiff(clients, await editorText(tab))
  assert.deepEqual(diff, { removed: ['  ipv6: true'], added: ['  ipv6: false'] })
  step('Mihomo 用 IPv6，客户端走 IPv4 changes one line', diff)

  // 关闭: back to the template plus the kept range.
  await choose(tab, '关闭')
  const off = await save(tab)
  diff = lineDiff(original, off)
  assert.deepEqual(diff, { removed: [], added: ['  fake-ip-range6: fc00::1/64'] })
  assert.deepEqual(server(off), { ...server(original), dns: { ...server(original).dns, 'fake-ip-range6': 'fc00::1/64' } })
  step('关闭 restores every line except the kept range', diff)

  assert.deepEqual(report.browserErrors, [])
  step('no browser errors')
} finally {
  await writeFile(join(evidence, 'report.json'), JSON.stringify(report, null, 2)).catch(() => {})
  await browser?.close().catch(() => {})
  await vite?.close().catch(() => {})
  backend?.kill()
  await rm(temporary, { recursive: true, force: true }).catch(() => {})
}
