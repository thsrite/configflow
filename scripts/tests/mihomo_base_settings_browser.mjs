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
// Choices and lists open in a small menu beside their cell, named after it (「DNS 方案：国内 DoH」).
const menu = (tab, name) => tab.getByRole('dialog', { name: new RegExp(`^${name}：`) })
async function openMenu(tab, trigger, name) {
  await trigger.click()
  await menu(tab, name).waitFor()
  await tab.waitForTimeout(250)
  return menu(tab, name)
}
async function shot(locator, name) {
  await locator.page().evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
  await locator.page().waitForTimeout(300)
  await locator.screenshot({ path: join(shots, `${name}.png`), animations: 'disabled' })
  step(`screenshot ${name}.png`)
}
// A section with its open menu: the menu is portalled, so clip the page to both boxes.
async function shotWith(tab, locators, name, pad = 12) {
  await tab.waitForTimeout(300)
  const boxes = await Promise.all(locators.map(item => item.boundingBox()))
  const left = Math.max(0, Math.min(...boxes.map(box => box.x)) - pad), top = Math.max(0, Math.min(...boxes.map(box => box.y)) - pad)
  const right = Math.max(...boxes.map(box => box.x + box.width)) + pad, bottom = Math.max(...boxes.map(box => box.y + box.height)) + pad
  await tab.screenshot({ path: join(shots, `${name}.png`), animations: 'disabled', clip: { x: left, y: top, width: right - left, height: bottom - top } })
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
      // A switch is operated through its 44px label row, a radio through its chip.
      const target = ['switch', 'radio'].includes(element.getAttribute('role')) ? element.closest('.min-h-11, label') || element : element
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
  // Every value is in view; nothing has to be unfolded first.
  assert.match(await section(desktop, 'controller').textContent(), /1 项提醒/)
  assert.match(await section(desktop, 'controller').locator('[data-base-controller-scope]').textContent(), /局域网可访问/)
  assert.match(await section(desktop, 'controller').textContent(), /默认密码 123456/)
  assert.equal(await section(desktop, 'ports').locator('#mihomo-base-mixed-port').inputValue(), '7893')
  assert.ok(await section(desktop, 'advanced').locator('#mihomo-base-tun-mtu').isVisible())
  // Element screenshots only capture what the editor's scroller shows, so this one uses a tall window.
  const tall = await page('light', { width: 1280, height: 2200 })
  await open(tall)
  // The editor window keeps its own height and scrolls; let it grow for this one picture.
  await tall.locator('[data-base-sheet]').evaluate(sheet => {
    for (let node = sheet.parentElement; node && node !== document.body; node = node.parentElement) {
      Object.assign(node.style, { maxHeight: 'none', height: 'auto', overflow: 'visible' })
      // The centred window would otherwise grow above the top of the page.
      if (getComputedStyle(node).position === 'fixed') {
        const left = node.getBoundingClientRect().left
        Object.assign(node.style, { top: '0', left: `${left}px`, translate: 'none', transform: 'none' })
      }
    }
  })
  await shot(tall.locator('[data-base-sheet]'), 'mihomo-base-settings-overview')
  await tall.context().close()

  // 控制面板: password hidden by default, listen choice in its menu, and the YAML follows.
  const controller = section(desktop, 'controller')
  const secret = controller.locator('#mihomo-base-secret')
  assert.equal(await secret.getAttribute('type'), 'password')
  await secret.fill('Lab-Pass_2026')
  let listen = await openMenu(desktop, controller.locator('[data-base-controller-scope]'), '控制面板监听')
  await listen.getByRole('radio', { name: '仅本机' }).click()
  await menu(desktop, '控制面板监听').waitFor({ state: 'hidden' })
  let text = await yaml(desktop)
  assert.match(text, /^secret: "Lab-Pass_2026"$/m)
  assert.match(text, /^external-controller: 127\.0\.0\.1:9090$/m)
  assert.match(text, /^# 控制面板$/m)
  listen = await openMenu(desktop, controller.locator('[data-base-controller-scope]'), '控制面板监听')
  await listen.getByRole('radio', { name: '局域网可访问' }).click()
  await desktop.waitForTimeout(300)
  assert.match(await yaml(desktop), /^external-controller: 0\.0\.0\.0:9090$/m)
  await controller.locator('[data-base-field="secret"] [data-field-help-trigger]').click()
  await desktop.locator('[data-field-help-content]').waitFor()
  assert.match(await desktop.locator('[data-field-help-content]').first().textContent(), /YAML 键：secret[\s\S]*【作用】[\s\S]*【注意】/)
  await controller.locator('[data-base-field="secret"] [data-field-help-trigger]').click()
  listen = await openMenu(desktop, controller.locator('[data-base-controller-scope]'), '控制面板监听')
  await shotWith(desktop, [controller, listen], 'mihomo-base-settings-controller')
  await desktop.keyboard.press('Escape')
  await menu(desktop, '控制面板监听').waitFor({ state: 'hidden' })
  assert.ok(await form.isVisible(), 'Escape closes only the menu')
  step('controller and password write the YAML')

  // 代理端口: a conflict blocks saving, with the reason under the field; Esc puts back the typed value.
  const ports = section(desktop, 'ports')
  await ports.locator('#mihomo-base-socks-port').fill('7893')
  await desktop.waitForTimeout(300)
  assert.match(await ports.textContent(), /端口 7893 同时被 mixed-port 和 socks-port 使用/)
  assert.ok(await saveButton(desktop).isDisabled(), 'saving is blocked by the conflict')
  await shot(ports, 'mihomo-base-settings-port-conflict')
  await ports.locator('#mihomo-base-socks-port').fill('7891')
  const mixed = ports.locator('#mihomo-base-mixed-port')
  await mixed.click()
  await mixed.fill('7899')
  await desktop.waitForTimeout(200)
  assert.match(await yaml(desktop), /^mixed-port: 7899$/m)
  await desktop.keyboard.press('Escape')
  await desktop.waitForTimeout(300)
  assert.equal(await mixed.inputValue(), '7893')
  assert.match(await yaml(desktop), /^mixed-port: 7893$/m)
  assert.ok(await form.isVisible(), 'Escape in a field does not leave the editor')
  await ports.getByRole('switch', { name: '启用TPROXY 端口' }).click()
  await desktop.waitForTimeout(300)
  assert.ok(await saveButton(desktop).isEnabled())
  assert.doesNotMatch(await yaml(desktop), /^tproxy-port:/m)
  step('port conflict blocks saving; Esc reverts a field; an optional port can be turned off')

  // DNS: switch presets in the menu and keep the user's nameserver-policy entry first.
  const dns = section(desktop, 'dns')
  const presets = await openMenu(desktop, dns.locator('[data-base-dns-preset]'), 'DNS 方案')
  await presets.getByRole('radio', { name: '国内外分开解析' }).click()
  await desktop.waitForTimeout(300)
  text = await yaml(desktop)
  assert.match(text, /'\+\.115\.com': '223\.5\.5\.5'[^\n]*\n[\s\S]*geosite:cn,private:/)
  assert.match(text, /direct-nameserver-follow-policy: true/)
  assert.match(await dns.locator('[data-base-dns-preset]').textContent(), /国内外分开解析/)
  const upstreams = await openMenu(desktop, dns.locator('[data-base-field="dns.nameserver"] button').last(), '默认 DNS')
  assert.match(await upstreams.locator('textarea').inputValue(), /https:\/\//)
  await shotWith(desktop, [dns, upstreams], 'mihomo-base-settings-dns')
  await desktop.keyboard.press('Escape')
  step('DNS preset switches and keeps the user policy entry; upstream lists open in a menu')

  // The read-only takeover keys.
  const managed = section(desktop, 'managed')
  assert.equal(await managed.locator('input, textarea, [role="switch"], button:not([data-field-help-trigger])').count(), 0)
  await shot(managed, 'mihomo-base-settings-managed')
  await saveButton(desktop).click()
  await desktop.getByText('基础配置草稿已保存').first().waitFor()
  const saved = await api('GET', '/custom-config/mihomo')
  assert.match(saved.data.config, /^secret: "Lab-Pass_2026"$/m)
  assert.match(saved.data.config, /geosite:cn,private/)
  assert.match(saved.data.config, /# 控制面板/)
  step('draft saved through the server checks, comments kept')

  // Unreadable YAML hides the sheet instead of overwriting it or showing defaults as values.
  await desktop.locator('.cm-content').click()
  await desktop.keyboard.press('ControlOrMeta+a')
  // The editor closes brackets and quotes by itself, so use a nested compact mapping.
  await desktop.keyboard.type('a: b: c')
  await form.locator('[data-base-yaml-error]').waitFor()
  assert.equal(await form.locator('[data-base-sheet]').count(), 0)
  assert.equal((await yaml(desktop)).trim(), 'a: b: c')
  assert.equal((await form.textContent()).split('YAML 无法解析：').length - 1, 1)
  await shot(form, 'mihomo-base-settings-yaml-error')
  step('unreadable YAML disables the form')

  // Phone: 390px, dark theme.
  const phone = await page('dark', { width: 390, height: 844 })
  await open(phone)
  await checkPhoneLayout(phone, 'sheet')
  await openMenu(phone, section(phone, 'dns').locator('[data-base-dns-preset]'), 'DNS 方案')
  const box = await menu(phone, 'DNS 方案').boundingBox()
  const phoneMenu = { left: box.x, right: box.x + box.width }
  assert.ok(phoneMenu.left >= 0 && phoneMenu.right <= 390, `menu fits the phone: ${JSON.stringify(phoneMenu)}`)
  await phone.keyboard.press('Escape')
  await section(phone, 'controller').scrollIntoViewIfNeeded()
  await phone.waitForTimeout(300)
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
