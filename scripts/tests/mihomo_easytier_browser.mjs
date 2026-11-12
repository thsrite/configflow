#!/usr/bin/env node
// EasyTier networking card in the real app: fill, checks, save, generated YAML, phone layout and screenshots.
// Test server only, from the task worktree:
//   cf-test typecheck   # syncs and links frontend/node_modules
//   rt --no-sync --back doc/img/guide --back .ci-results/mihomo-easytier -- node scripts/tests/mihomo_easytier_browser.mjs
// Real Vue frontend and Flask API on a temporary data directory (no login, no Agent, nothing published).
// Example addresses only (RFC 5737 / private ranges).
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run this on the test server')
const root = process.cwd()
const shots = resolve(root, process.env.CF_SHOTS || 'doc/img/guide')
const evidence = resolve(root, '.ci-results/mihomo-easytier')
const apiPort = Number(process.env.CF_API_PORT || 15941)
const webPort = Number(process.env.CF_WEB_PORT || 15942)
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const temporary = await mkdtemp(join(tmpdir(), 'configflow-easytier-'))
const report = { steps: [], browserErrors: [], layout: [] }
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
async function page(theme, viewport) {
  const mobile = viewport.width < 500
  const context = await browser.newContext({ viewport, deviceScaleFactor: mobile ? 2 : 1, colorScheme: theme, hasTouch: mobile, isMobile: mobile })
  await context.addInitScript(mode => { localStorage.setItem('configflow-theme', mode) }, theme)
  const tab = await context.newPage()
  tab.on('pageerror', error => report.browserErrors.push(error.message))
  return tab
}
async function open(tab) {
  await tab.goto(`${base}/generate`)
  await tab.getByRole('button', { name: '基础配置' }).first().click()
  await tab.locator('[data-mihomo-easytier]').waitFor({ timeout: 30000 })
  await tab.waitForTimeout(400)
}
const card = tab => tab.locator('[data-mihomo-easytier]')
const saveButton = tab => tab.locator('[data-settings-publish-bar]').getByRole('button', { name: '保存草稿' })
async function typeIn(tab, selector, text) {
  const input = card(tab).locator(selector)
  await input.click()
  await input.fill(text)
  await input.press('Enter')
}
// A list cell opens a small editor beside it; one entry per line.
async function fillList(tab, field, lines) {
  // The cell's last button opens the list (the first one is its help).
  await card(tab).locator(`[data-base-field="easytier.${field}"] button`).last().click()
  const area = tab.locator('[role="dialog"] textarea').last()
  await area.waitFor()
  await area.fill(lines.join('\n'))
  await tab.keyboard.press('Escape')
  await tab.waitForTimeout(200)
}
async function shot(tab, locator, name) {
  await tab.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
  await tab.waitForTimeout(300)
  await locator.screenshot({ path: join(shots, `${name}.png`), animations: 'disabled' })
  step(`screenshot ${name}.png`)
}
async function checkPhoneLayout(tab, label) {
  const result = await tab.evaluate(() => {
    const form = document.querySelector('[data-mihomo-easytier]')
    const scroller = form.closest('.overflow-y-auto') || document.scrollingElement
    const small = []
    for (const element of form.querySelectorAll('button, input, textarea, a, [role="switch"]')) {
      const box = element.getBoundingClientRect()
      if (!box.width || !box.height || getComputedStyle(element).visibility === 'hidden') continue
      if (element.closest('.cf-compact-help')) continue // help icons follow the shared sheet rule
      const target = element.getAttribute('role') === 'switch' ? element.closest('.min-h-11, label') || element : element
      const area = target.getBoundingClientRect()
      if (area.height < 43.5 || area.width < 43.5) small.push(`${element.tagName.toLowerCase()} ${element.getAttribute('aria-label') || element.textContent.trim().slice(0, 20)} ${Math.round(area.width)}x${Math.round(area.height)}`)
    }
    const wide = [...form.querySelectorAll('*')].filter(element => element.getBoundingClientRect().right > window.innerWidth + 0.5)
      .map(element => element.tagName.toLowerCase() + (element.className && typeof element.className === 'string' ? '.' + element.className.split(' ')[0] : ''))
    return { pageScroll: document.documentElement.scrollWidth - window.innerWidth, scroller: scroller.scrollWidth - scroller.clientWidth, small, wide: wide.slice(0, 10) }
  })
  report.layout.push({ label, ...result })
  assert.ok(result.pageScroll <= 0 && result.scroller <= 0, `${label}: horizontal scroll ${JSON.stringify(result)}`)
  assert.deepEqual(result.wide, [], `${label}: wider than the screen`)
  assert.deepEqual(result.small, [], `${label}: touch targets under 44px`)
  step(`phone layout ${label}`)
}

try {
  await mkdir(shots, { recursive: true })
  await mkdir(evidence, { recursive: true })
  const launcher = `from pathlib import Path
import json, os
from backend.common.config import get_default_config, DATA_DIR
from backend.common.setup_flow import initial_mihomo_base
config = get_default_config()
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

  const initial = await api('GET', '/custom-config/mihomo')
  assert.equal(initial.status, 200)
  assert.equal(initial.data.easytier.enabled, false)
  const refused = await api('POST', '/custom-config/mihomo', { config: initial.data.config,
    easytier: { enabled: true, options: { 'network-name': 'home-lab', peers: ['tcp://203.0.113.10:11010'], 'proxy-networks': ['192.168.0.0/24->10.200.0.0/24'] } } })
  assert.equal(refused.status, 400)
  assert.match(refused.data.message, /->/)
  step('server refuses a mapped proxy network')

  const desktop = await page('light', { width: 1280, height: 900 })
  await open(desktop)
  assert.equal(await card(desktop).locator('[data-easytier-form]').count(), 0)
  await card(desktop).locator('[data-easytier-enabled]').click()
  await card(desktop).locator('[data-easytier-form]').waitFor()
  // Enabled without a network name or peers: blocked, the same as on the server.
  assert.ok(await saveButton(desktop).isDisabled(), 'save blocked while required fields are empty')
  assert.match(await card(desktop).textContent(), /请填写网络名/)
  step('enabling without required fields blocks saving')

  await typeIn(desktop, '[data-base-field="easytier.network-name"] input', 'home-lab')
  await typeIn(desktop, '[data-base-field="easytier.network-secret"] input', 'example-secret')
  await typeIn(desktop, '[data-base-field="easytier.hostname"] input', 'laptop')
  await fillList(desktop, 'peers', ['tcp://203.0.113.10:11010', 'tcp://[2001:db8::10]:11010'])
  await fillList(desktop, 'routes', ['10.144.144.0/24', '10.200.0.0/24'])
  await card(desktop).locator('[data-base-field="easytier.magic_dns"] [role="switch"]').click()
  assert.ok(!(await saveButton(desktop).isDisabled()), 'save allowed once filled')
  assert.match(await card(desktop).locator('[data-base-field="easytier.apply_to"]').textContent(), /订阅和下载/)
  step('filled network, secret, peers and routes')

  // A mapped network in the advanced fields is refused in place.
  await card(desktop).locator('[data-easytier-advanced]').click()
  await fillList(desktop, 'proxy-networks', ['192.168.0.0/24->10.200.0.0/24'])
  assert.ok(await saveButton(desktop).isDisabled(), 'mapped network blocks saving')
  assert.match(await card(desktop).locator('[data-base-field="easytier.proxy-networks"]').textContent(), /不支持「->」/)
  await fillList(desktop, 'proxy-networks', [])
  await card(desktop).locator('[data-base-field="easytier.secure-mode"] [role="switch"]').click()
  await card(desktop).locator('[data-base-field="easytier.private-mode"] [role="switch"]').click()
  assert.ok(!(await saveButton(desktop).isDisabled()))
  step('mapped proxy network refused in the form')
  await card(desktop).locator('[data-easytier-advanced]').click()

  await shot(desktop, card(desktop), 'mihomo-easytier-settings')
  await saveButton(desktop).click()
  await desktop.waitForTimeout(1200)
  const saved = await api('GET', '/custom-config/mihomo')
  assert.deepEqual(saved.data.easytier.options, {
    'network-name': 'home-lab', 'network-secret': 'example-secret', hostname: 'laptop',
    peers: ['tcp://203.0.113.10:11010', 'tcp://[2001:db8::10]:11010'], 'secure-mode': true, 'private-mode': true, udp: true })
  assert.deepEqual(saved.data.easytier.routes, ['10.144.144.0/24', '10.200.0.0/24'])
  assert.equal(saved.data.easytier.magic_dns, true)
  step('draft saved')

  const preview = await api('POST', '/generate/mihomo/preview', { base_url: 'http://config.example' })
  assert.equal(preview.status, 200, JSON.stringify(preview.data))
  const content = preview.data.content
  assert.match(content, /type: easytier/)
  assert.match(content, /network-name: home-lab/)
  assert.match(content, /- IP-CIDR,10\.144\.144\.0\/24,EasyTier,no-resolve/)
  assert.match(content, /\+\.et\.net: et:\/\/EasyTier/)
  const rules = content.split('\nrules:\n')[1]
  assert.match(rules.split('\n')[0], /IP-CIDR,10\.144\.144\.0\/24,EasyTier/)
  await writeFile(join(evidence, 'generated.yaml'), content)
  step('generated config has the node, first rules and MagicDNS')

  // Reload: the saved setting comes back into the form unchanged (not dirty).
  await open(desktop)
  assert.equal(await card(desktop).locator('[data-base-field="easytier.network-name"] input').inputValue(), 'home-lab')
  assert.equal(await card(desktop).locator('[data-base-field="easytier.network-secret"] input').getAttribute('type'), 'password')
  step('saved setting reloads')

  const phone = await page('dark', { width: 390, height: 844 })
  await open(phone)
  await card(phone).scrollIntoViewIfNeeded()
  await checkPhoneLayout(phone, 'card')
  await card(phone).locator('[data-easytier-advanced]').click()
  await checkPhoneLayout(phone, 'card with more options')
  await card(phone).locator('[data-easytier-advanced]').click()
  await card(phone).evaluate(element => element.scrollIntoView({ block: 'start' }))
  await phone.waitForTimeout(300)
  await phone.screenshot({ path: join(shots, 'mihomo-easytier-settings-mobile-dark.png'), animations: 'disabled' })
  step('screenshot mihomo-easytier-settings-mobile-dark.png')

  assert.deepEqual(report.browserErrors, [])
  step('no browser errors')
} finally {
  await writeFile(join(evidence, 'report.json'), JSON.stringify(report, null, 2)).catch(() => {})
  await browser?.close().catch(() => {})
  await vite?.close().catch(() => {})
  backend?.kill()
  await rm(temporary, { recursive: true, force: true }).catch(() => {})
}
