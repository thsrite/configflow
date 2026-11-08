#!/usr/bin/env node
// Mihomo base settings form in the real app: YAML sync, checks, save, phone layout and screenshots.
// Test server only, from the task worktree:
//   cf-test typecheck   # syncs and links frontend/node_modules
//   rt --no-sync --back doc/img/guide --back .ci-results/mihomo-base-settings -- node scripts/tests/mihomo_base_settings_browser.mjs
// Real Vue frontend and Flask API on a temporary data directory (no login, no Agent, nothing published).
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run this on the test server')
const root = process.cwd()
const shots = resolve(root, process.env.CF_SHOTS || 'doc/img/guide')
const evidence = resolve(root, '.ci-results/mihomo-base-settings')
const apiPort = Number(process.env.CF_API_PORT || 15931)
const webPort = Number(process.env.CF_WEB_PORT || 15932)
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const temporary = await mkdtemp(join(tmpdir(), 'configflow-base-settings-'))
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
  await tab.locator('[data-mihomo-base-settings]').waitFor({ timeout: 30000 })
  await tab.waitForTimeout(400)
}
const yaml = tab => tab.$eval('.cm-content', el => [...el.querySelectorAll('.cm-line')].map(line => line.textContent).join('\n'))
const section = (tab, name) => tab.locator(`[data-base-section="${name}"]`)
async function expand(tab, name) {
  const item = section(tab, name)
  if (await item.getAttribute('data-state') !== 'open') await item.locator('button[aria-label]').first().click()
  await tab.waitForTimeout(250)
  return item
}
async function shot(locator, name) {
  await locator.page().evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
  await locator.page().waitForTimeout(300)
  await locator.screenshot({ path: join(shots, `${name}.png`), animations: 'disabled' })
  step(`screenshot ${name}.png`)
}
const saveButton = tab => tab.locator('[data-settings-publish-bar]').getByRole('button', { name: '保存草稿' })

// No horizontal scroll and 44px touch targets for every control in the form.
async function checkPhoneLayout(tab, label) {
  const result = await tab.evaluate(() => {
    const form = document.querySelector('[data-mihomo-base-settings]')
    const scroller = form.closest('.overflow-y-auto') || document.scrollingElement
    const small = []
    for (const element of form.querySelectorAll('button, input, textarea, [role="combobox"], [role="switch"]')) {
      const box = element.getBoundingClientRect()
      if (!box.width || !box.height || getComputedStyle(element).visibility === 'hidden') continue
      // A switch is operated through its 44px label row.
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

  // Server checks first: the same rules hold for a save that bypasses the form.
  const initial = await api('GET', '/custom-config/mihomo')
  assert.equal(initial.status, 200)
  assert.equal(initial.data.base_settings.takeover, null)
  const template = initial.data.config
  assert.match(template, /secret: "123456"/)
  let rejected = await api('POST', '/custom-config/mihomo', { config: template.replace('socks-port: 7891', 'socks-port: 7893') })
  assert.equal(rejected.status, 400)
  assert.match(rejected.data.message, /端口 7893 同时被 mixed-port 和 socks-port 使用/)
  rejected = await api('POST', '/custom-config/mihomo', { config: template.replace('secret: "123456"', 'secret: "short"') })
  assert.equal(rejected.status, 400)
  assert.match(rejected.data.message, /8–64/)
  rejected = await api('POST', '/custom-config/mihomo', { config: template.replace('    - https://doh.pub/dns-query\n    - https://dns.alidns.com/dns-query\n\n  # DNS', '    - system\n\n  # DNS') })
  assert.equal(rejected.status, 400)
  assert.match(rejected.data.message, /dns\.proxy-server-nameserver 中的 system 指向本机或系统 DNS/)
  step('server rejects a port conflict, a weak password and a DNS loop')

  const desktop = await page('light', { width: 1280, height: 1000 })
  await open(desktop)
  const form = desktop.locator('[data-mihomo-base-settings]')
  assert.equal(await form.locator('[data-base-section]').count(), 9)
  assert.match(await section(desktop, 'controller').locator('[data-base-summary]').textContent(), /0\.0\.0\.0:9090 · 默认密码 123456/)
  assert.match(await section(desktop, 'controller').textContent(), /1 项提醒/)
  await shot(form, 'mihomo-base-settings-overview')

  // 控制面板: password hidden by default, listen choice and the YAML follow.
  const controller = await expand(desktop, 'controller')
  const secret = controller.locator('#mihomo-base-secret')
  assert.equal(await secret.getAttribute('type'), 'password')
  await secret.fill('Lab-Pass_2026')
  await controller.getByRole('radio', { name: '仅本机' }).click()
  await desktop.waitForTimeout(300)
  let text = await yaml(desktop)
  assert.match(text, /^secret: "Lab-Pass_2026"$/m)
  assert.match(text, /^external-controller: 127\.0\.0\.1:9090$/m)
  assert.match(text, /^# 控制面板$/m)
  await controller.getByRole('radio', { name: '局域网可访问' }).click()
  await controller.locator('[data-base-field="secret"] [data-field-help-trigger]').click()
  await desktop.locator('[data-field-help-content]').waitFor()
  assert.match(await desktop.locator('[data-field-help-content]').first().textContent(), /【作用】[\s\S]*【注意】/)
  await shot(controller, 'mihomo-base-settings-controller')
  // Escape here would ask to leave the editor once the help has closed; toggle the help instead.
  if (await desktop.locator('[data-field-help-content]').count()) await controller.locator('[data-base-field="secret"] [data-field-help-trigger]').click()
  step('controller and password write the YAML')

  // 代理端口: a conflict blocks saving, with the reason under both fields.
  const ports = await expand(desktop, 'ports')
  await ports.locator('#mihomo-base-socks-port').fill('7893')
  await desktop.waitForTimeout(300)
  assert.match(await ports.textContent(), /端口 7893 同时被 mixed-port 和 socks-port 使用/)
  assert.ok(await saveButton(desktop).isDisabled(), 'saving is blocked by the conflict')
  await shot(ports, 'mihomo-base-settings-port-conflict')
  await ports.locator('#mihomo-base-socks-port').fill('7891')
  await ports.getByRole('switch', { name: '启用TPROXY 端口（tproxy-port）' }).click()
  await desktop.waitForTimeout(300)
  assert.ok(await saveButton(desktop).isEnabled())
  assert.doesNotMatch(await yaml(desktop), /^tproxy-port:/m)
  step('port conflict blocks saving; an optional port can be turned off')

  // DNS: switch presets and keep the user's nameserver-policy entry first.
  const dns = await expand(desktop, 'dns')
  await dns.getByRole('radio', { name: '国内外分开解析' }).click()
  await desktop.waitForTimeout(300)
  text = await yaml(desktop)
  assert.match(text, /'\+\.115\.com': '223\.5\.5\.5'[^\n]*\n[\s\S]*geosite:cn,private:/)
  assert.match(text, /direct-nameserver-follow-policy: true/)
  assert.match(await section(desktop, 'dns').locator('[data-base-summary]').textContent(), /国内外分开解析/)
  await shot(dns, 'mihomo-base-settings-dns')
  await dns.getByRole('radio', { name: '自定义' }).click()
  await dns.locator('[data-base-dns-custom]').waitFor()
  step('DNS preset switches and keeps the user policy entry')

  // 进阶 and the read-only takeover keys.
  await expand(desktop, 'advanced')
  const managed = await expand(desktop, 'managed')
  assert.equal(await managed.locator('input, textarea, [role="switch"]').count(), 0)
  await shot(managed, 'mihomo-base-settings-managed')
  await saveButton(desktop).click()
  await desktop.getByText('基础配置草稿已保存').first().waitFor()
  const saved = await api('GET', '/custom-config/mihomo')
  assert.match(saved.data.config, /^secret: "Lab-Pass_2026"$/m)
  assert.match(saved.data.config, /geosite:cn,private/)
  assert.match(saved.data.config, /# 控制面板/)
  step('draft saved through the server checks, comments kept')

  // Unreadable YAML disables the form instead of overwriting it.
  await desktop.locator('.cm-content').click()
  await desktop.keyboard.press('ControlOrMeta+a')
  // The editor closes brackets and quotes by itself, so use a nested compact mapping.
  await desktop.keyboard.type('a: b: c')
  await form.locator('[data-base-yaml-error]').waitFor()
  assert.equal(await form.locator('[inert]').count() > 0, true)
  assert.equal((await yaml(desktop)).trim(), 'a: b: c')
  await shot(form, 'mihomo-base-settings-yaml-error')
  step('unreadable YAML disables the form')

  // Phone: 390px, dark theme.
  const phone = await page('dark', { width: 390, height: 844 })
  await open(phone)
  await checkPhoneLayout(phone, 'folded')
  for (const name of ['controller', 'ports', 'dns', 'sniffer', 'advanced', 'managed']) await expand(phone, name)
  await phone.locator('[data-base-section="dns"]').getByRole('radio', { name: '自定义' }).click()
  await phone.waitForTimeout(300)
  await checkPhoneLayout(phone, 'expanded')
  await section(phone, 'controller').scrollIntoViewIfNeeded()
  await phone.screenshot({ path: join(shots, 'mihomo-base-settings-mobile-dark.png'), animations: 'disabled' })
  step('screenshot mihomo-base-settings-mobile-dark.png')

  assert.deepEqual(report.browserErrors, [])
  step('no browser errors')
} finally {
  await writeFile(join(evidence, 'report.json'), JSON.stringify(report, null, 2)).catch(() => {})
  await browser?.close().catch(() => {})
  await vite?.close().catch(() => {})
  backend?.kill()
  await rm(temporary, { recursive: true, force: true }).catch(() => {})
}
