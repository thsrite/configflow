#!/usr/bin/env node
// Radio chips in the Mihomo settings (IPv6 mode, 控制面板监听, DNS 方案) in the real app: keyboard, phone layout,
// the same look as the Agent install dialog, and the IPv6 mode screenshots. Test server only, from the task worktree:
//   cf-test typecheck   # syncs and links frontend/node_modules
//   rt --no-sync --back doc/img/guide --back .ci-results/mihomo-radio-chips -- node scripts/tests/mihomo_settings_radio_chips_browser.mjs
// Real Vue frontend and Flask API on a temporary data directory (no login, nothing published). There is no device:
// the Agent list, device check, verification and upstream router answers for one demo Mihomo and one demo RouterOS
// are simulated in the browser (Playwright routes), so the step list shows the same states as the lab screenshots.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run this on the test server')
const root = process.cwd()
const shots = resolve(root, process.env.CF_SHOTS || 'doc/img/guide')
const evidence = resolve(root, '.ci-results/mihomo-radio-chips')
const apiPort = Number(process.env.CF_API_PORT || 15941)
const webPort = Number(process.env.CF_WEB_PORT || 15942)
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const temporary = await mkdtemp(join(tmpdir(), 'configflow-radio-chips-'))
const report = { steps: [], browserErrors: [], styles: {} }
const base = `http://127.0.0.1:${webPort}`
const AGENT = 'agent-demo-mihomo'
const ROUTER = 'router-demo-ros'
const NODES = 'Mihomo 用 IPv6，客户端走 IPv4'
const CLIENTS = '客户端也走 IPv6'
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

// ---- Simulated device answers -------------------------------------------------------------------------------
// One scene at a time; each saved draft moves the scene on, like the lab after a save.
let scene = 'nodes'
let revision = 1
const ok = (id, label = id) => ({ id, label, status: 'ok', message: `${label}正常` })
function readiness() {
  const dns6 = scene === 'clients'
  const deployed = { nodes: '已保存草稿与设备配置不同：IPv6 Fake-IP 网段。请通过保存并发布应用配置。',
    clients: '已保存草稿与设备配置不同：DNS IPv6。请通过保存并发布应用配置。' }[scene]
  return {
    success: true, agent_id: AGENT, profile_id: 'default', config_revision: revision,
    configuration: { ipv6: true, dns_ipv6: dns6, fake_ipv6_range: 'fc00::1/64', errors: [] },
    host: { core: { ipv6: true, 'dns.ipv6': dns6 } },
    checks: [ok('configuration', '保存的配置'), ok('host_address', 'IPv6 地址'), ok('host_default_route', 'IPv6 默认路由'), ok('host_forwarding_ra', '转发与 RA'),
      ...(deployed ? [{ id: 'deployed_config', label: '设备上的配置', status: 'warning', message: deployed }] : [])],
    routers: [{ id: ROUTER, name: '测试 ROS', status: 'ok', can_prepare: false, checks: [ok('fake_ipv6', 'IPv6 Fake-IP 路由'), { ...ok('fake6_0', '客户端路径'), decision: 'allow' }] }],
    can_prepare: false, prepare_interface: 'eth0', check_token: 'demo', connectivity: { status: 'not_checked', message: '尚未检查' },
    checked_at: new Date().toISOString(), cached: false,
    publication: scene === 'changed' ? 'changed' : 'synced',
  }
}
function upstream() {
  const pending = scene === 'nodes' || scene === 'clients'
  return {
    success: true, agent_id: AGENT, agent_profile_id: 'default', checked_at: new Date().toISOString(), cached: false,
    routers: [{ id: ROUTER, name: '测试 ROS', type: 'routeros', profile_id: 'default', profile_name: '默认', state: pending ? 'pending' : 'synced',
      pending: pending ? 2 : 0, message: '',
      items: pending ? [{ key: 'ipv6_fakeip', label: 'IPv6 Fake-IP 分流路由', status: '待添加', count: 1 }, { key: 'ipv6_ruleset', label: 'IPv6 规则集路由', status: '待添加', count: 1 }] : [] }],
  }
}
function verification() {
  if (scene !== 'passed') return { success: true, agent_id: AGENT, profile_id: 'default', verification: null, applicable: true }
  const ids = ['host_address', 'host_default_route', 'runtime_proxy_http', 'runtime_node_ipv6_transport', 'client_http_ipv4', 'client_mihomo_path', 'client_ros_path', 'client_ipv6_transport']
  return { success: true, agent_id: AGENT, profile_id: 'default', applicable: true,
    verification: { status: 'ok', scope: 'client_path', config_revision: revision, checked_at: new Date().toISOString(), checks: ids.map(id => ok(id)) } }
}
const agent = { id: AGENT, name: '测试 Mihomo', host: 'mihomo.example', port: 8080, service_type: 'mihomo', profile_id: 'default', status: 'online', enabled: true, version: '1.8.54-go' }
async function simulate(context) {
  await context.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) return route.continue()
    const path = url.pathname
    const json = data => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(data) })
    if (path === '/api/agents' && route.request().method() === 'GET') return json([agent])
    if (path === `/api/agents/${AGENT}/ipv6-readiness`) return json(readiness())
    if (path === `/api/agents/${AGENT}/ipv6-readiness/verify` && route.request().method() === 'GET') return json(verification())
    if (path === `/api/agents/${AGENT}/ipv6-readiness/verification-settings`) {
      return json({ success: true, agent_id: AGENT, profile_id: 'default', settings: { client_agent_id: '', test_url: '' }, candidates: [] })
    }
    if (path === `/api/agents/${AGENT}/upstream-routers`) return json(upstream())
    if (path === '/api/routers' && route.request().method() === 'GET') return json([{ id: ROUTER, name: '测试 ROS', type: 'routeros', profile_id: 'default', proxy_gateway_agent_id: AGENT }])
    // Nothing may reach a device: everything else is the real API on temporary data.
    assert.ok(!path.startsWith(`/api/agents/${AGENT}`), `unexpected device call ${path}`)
    return route.continue()
  })
}

// ---- Browser helpers ------------------------------------------------------------------------------------------
async function page(theme, viewport, { devices = true } = {}) {
  const mobile = viewport.width < 500
  const context = await browser.newContext({ viewport, deviceScaleFactor: mobile ? 2 : 1, colorScheme: theme, hasTouch: mobile, isMobile: mobile })
  await context.addInitScript(mode => { localStorage.setItem('configflow-theme', mode) }, theme)
  if (devices) await simulate(context)
  const tab = await context.newPage()
  tab.on('pageerror', error => report.browserErrors.push(error.message))
  return tab
}
async function openSettings(tab) {
  await tab.goto(`${base}/generate`)
  await tab.getByRole('button', { name: '基础配置' }).first().click()
  await tab.locator('[data-mihomo-ipv6-settings]').waitFor({ timeout: 30000 })
  await tab.waitForTimeout(400)
}
const modes = tab => tab.locator('[role="radiogroup"][aria-label="IPv6 模式"]')
const modeName = tab => modes(tab).evaluate(group => [...group.querySelectorAll('label')]
  .filter(label => label.querySelector('[role="radio"]')?.getAttribute('aria-checked') === 'true').map(label => label.textContent.trim()).join(''))
const chip = (tab, label) => modes(tab).locator('label', { hasText: new RegExp(`^${label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`) })
const yaml = tab => tab.$eval('.cm-content', el => [...el.querySelectorAll('.cm-line')].map(line => line.textContent).join('\n'))
async function saveDraft(tab, next) {
  scene = next
  revision++
  await tab.locator('[data-settings-publish-bar]').getByRole('button', { name: '保存草稿' }).click()
  await tab.getByText('基础配置草稿已保存').first().waitFor()
}
// The steps have read their data: nothing in progress and the verify row is there.
async function settle(tab) {
  await tab.waitForTimeout(300)
  await tab.waitForFunction(() => {
    const steps = document.querySelector('[data-mihomo-ipv6-steps]')
    return !steps || (!steps.querySelector('[data-tone="progress"]') && !!steps.querySelector('[data-ipv6-step="verify"]'))
  }, null, { timeout: 60000 })
  await tab.waitForTimeout(600)
}
async function quiet(tab) {
  await tab.waitForFunction(() => !document.querySelector('[data-sonner-toast]'), null, { timeout: 15000 })
  await tab.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
  // No hover left on the last button clicked.
  await tab.mouse.move(0, 0)
  await tab.waitForTimeout(300)
}
async function top(tab) {
  await tab.$eval('[data-mihomo-ipv6-settings]', el => el.scrollIntoView({ block: 'start' }))
  await tab.waitForTimeout(300)
}
// Up to `height` of the IPv6 section, at its full width.
async function sectionShot(tab, name, height) {
  const box = await tab.locator('[data-mihomo-ipv6-settings]').boundingBox()
  await tab.screenshot({ path: join(shots, `${name}.png`), animations: 'disabled',
    clip: { x: box.x, y: box.y, width: box.width, height: height ? Math.min(height, box.height) : box.height } })
  step(`screenshot ${name}.png`, { width: Math.round(box.width), height: Math.round(height ? Math.min(height, box.height) : box.height) })
}

// What makes a chip look like a chip; compared with the Agent install dialog.
const LOOK = ['height', 'paddingTop', 'paddingLeft', 'borderTopLeftRadius', 'borderTopWidth', 'borderTopColor', 'backgroundColor', 'color', 'fontSize', 'columnGap', 'cursor']
const chipStyles = (tab, group) => tab.locator(group).evaluate((element, keys) => [...element.querySelectorAll('label')].map(label => {
  const style = getComputedStyle(label)
  const radio = label.querySelector('[role="radio"]')
  const dot = getComputedStyle(radio)
  return { checked: radio.getAttribute('aria-checked') === 'true', ...Object.fromEntries(keys.map(key => [key, style[key]])),
    dot: { width: dot.width, borderColor: dot.borderTopColor, radius: dot.borderTopLeftRadius } }
}), LOOK)
const pick = list => ({ on: list.find(item => item.checked), off: list.find(item => !item.checked) })
function sameLook(ours, theirs, label) {
  for (const state of ['on', 'off']) {
    const { height: ourHeight, checked: _a, ...ourLook } = ours[state]
    const { height: theirHeight, checked: _b, ...theirLook } = theirs[state]
    assert.deepEqual(ourLook, theirLook, `${label} ${state}: chip looks differ`)
    assert.ok(Number.parseFloat(ourHeight) >= 44 && Number.parseFloat(theirHeight) >= 44, `${label} ${state}: ${ourHeight} / ${theirHeight}`)
  }
}

// No horizontal scroll, every chip at least 44px, and the long option fully inside its chip.
async function checkPhone(tab, label) {
  const result = await tab.evaluate(() => {
    const settings = document.querySelector('[data-mihomo-ipv6-settings]')
    const scroller = settings.closest('.overflow-y-auto') || document.scrollingElement
    const chips = [...document.querySelectorAll('[role="radiogroup"][aria-label="IPv6 模式"] label')].map(item => {
      const box = item.getBoundingClientRect()
      return { text: item.textContent.trim(), left: Math.round(box.left), right: Math.round(box.right), width: Math.round(box.width), height: Math.round(box.height),
        overflow: item.scrollWidth - item.clientWidth }
    })
    const wide = [...settings.querySelectorAll('*')].filter(element => element.getBoundingClientRect().right > window.innerWidth + 0.5)
      .map(element => element.tagName.toLowerCase())
    return { pageScroll: document.documentElement.scrollWidth - window.innerWidth, scroller: scroller.scrollWidth - scroller.clientWidth, chips, wide: wide.slice(0, 10) }
  })
  assert.ok(result.pageScroll <= 0 && result.scroller <= 0, `${label}: horizontal scroll ${JSON.stringify(result)}`)
  assert.deepEqual(result.wide, [], `${label}: wider than the screen`)
  assert.deepEqual(result.chips.map(item => item.text), ['关闭', NODES, CLIENTS])
  for (const item of result.chips) {
    assert.ok(item.height >= 44 && item.width >= 44, `${label}: chip ${item.text} ${item.width}x${item.height}`)
    assert.ok(item.overflow <= 0, `${label}: ${item.text} is cut off`)
  }
  step(`phone layout ${label}`, result)
  return result
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
  assert.match(initial.data.config, /^ipv6: false$/m)
  assert.doesNotMatch(initial.data.config, /fake-ip-range6/)

  // 1. Desktop, light: the chips, the keyboard, and the YAML they write.
  const desktop = await page('light', { width: 1440, height: 1000 })
  await openSettings(desktop)
  assert.deepEqual(await modes(desktop).locator('label').allTextContents(), ['关闭', NODES, CLIENTS])
  assert.equal(await modeName(desktop), '关闭')
  // Label above, chips below it, the description under the chips; all start at the same left edge.
  const layout = await desktop.evaluate(() => {
    const group = document.querySelector('[role="radiogroup"][aria-label="IPv6 模式"]')
    const field = group.closest('.cf-form-field')
    const heading = field.querySelector('.cf-field-heading').getBoundingClientRect()
    const chips = group.getBoundingClientRect()
    const description = document.querySelector('[data-ipv6-mode-description]').getBoundingClientRect()
    return { heading: { left: heading.left, bottom: heading.bottom }, chips: { left: chips.left, top: chips.top, bottom: chips.bottom }, description: { left: description.left, top: description.top } }
  })
  assert.ok(layout.chips.top >= layout.heading.bottom && layout.description.top >= layout.chips.bottom, JSON.stringify(layout))
  assert.equal(Math.round(layout.chips.left), Math.round(layout.heading.left))
  assert.equal(Math.round(layout.description.left), Math.round(layout.chips.left))
  step('label above, chips, description below, left-aligned', layout)

  // Tab lands on the checked chip; the arrow keys move the choice and skip nothing that is enabled.
  await desktop.locator('[role="radiogroup"][aria-label="IPv6 模式"] [role="radio"][aria-checked="true"]').focus()
  // A key held as briefly as a person does: reka-ui checks the focused radio a moment after the keydown.
  await desktop.keyboard.press('ArrowRight', { delay: 80 })
  await desktop.waitForTimeout(400)
  assert.equal(await modeName(desktop), NODES)
  assert.equal(await desktop.evaluate(() => document.activeElement?.closest('label')?.textContent.trim()), NODES)
  let text = await yaml(desktop)
  assert.match(text, /^ipv6: true$/m)
  assert.match(text, /^ {2}ipv6: false$/m)
  assert.match(text, /^ {2}fake-ip-range6: fc00::1\/64$/m)
  await desktop.keyboard.press('ArrowLeft', { delay: 80 })
  await desktop.waitForTimeout(400)
  assert.equal(await modeName(desktop), '关闭')
  await desktop.keyboard.press('ArrowLeft', { delay: 80 })   // wraps around to the last option
  await desktop.waitForTimeout(400)
  assert.equal(await modeName(desktop), CLIENTS)
  await chip(desktop, NODES).click()
  await desktop.waitForTimeout(400)
  assert.equal(await modeName(desktop), NODES)
  step('arrow keys and clicks choose the IPv6 mode and write the YAML')

  // The range input sits under the chips, at the same left edge.
  const range = desktop.getByLabel('IPv6 Fake-IP 网段', { exact: true })
  assert.equal(Math.round((await range.boundingBox()).x), Math.round(layout.chips.left))
  await range.fill('fc00:0:0:2::1/64')
  await range.blur()
  assert.match(await yaml(desktop), /fake-ip-range6: fc00:0:0:2::1\/64/)
  await saveDraft(desktop, 'nodes')
  await settle(desktop)
  await top(desktop)
  await quiet(desktop)
  await desktop.screenshot({ path: join(shots, 'mihomo-ipv6-mode-nodes.png'), animations: 'disabled' })
  step('screenshot mihomo-ipv6-mode-nodes.png')

  // 2. 客户端也走 IPv6.
  await chip(desktop, CLIENTS).click()
  await desktop.waitForTimeout(400)
  text = await yaml(desktop)
  assert.match(text, /^ {2}ipv6: true$/m)
  assert.equal(text.match(/fake-ip-range6:/g).length, 1)
  await saveDraft(desktop, 'clients')
  await settle(desktop)
  await top(desktop)
  await quiet(desktop)
  await desktop.screenshot({ path: join(shots, 'mihomo-ipv6-mode-steps.png'), animations: 'disabled' })
  step('screenshot mihomo-ipv6-mode-steps.png')

  // 3. A handwritten combination outside the three modes: no chip selected, the reason in the warning color.
  const line = desktop.locator('.cm-line', { hasText: /^ipv6: true$/ }).first()
  await line.click()
  await desktop.keyboard.press('End')
  for (let index = 0; index < 4; index++) await desktop.keyboard.press('Backspace')
  await desktop.keyboard.type('false')
  await desktop.waitForTimeout(500)
  assert.equal(await modeName(desktop), '')
  assert.equal(await modes(desktop).locator('[role="radio"][aria-checked="true"]').count(), 0)
  assert.equal(await modes(desktop).locator('[role="radio"]:disabled').count(), 0)
  assert.match(await desktop.locator('[data-ipv6-mode-custom]').innerText(), /^自定义：/)
  assert.equal(await desktop.locator('[data-ipv6-mode-custom]').evaluate(el => el.classList.contains('text-warning-accent')), true)
  assert.match(await yaml(desktop), /^ipv6: false$/m, 'not rewritten by itself')
  await top(desktop)
  await desktop.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
  await desktop.screenshot({ path: join(shots, 'mihomo-ipv6-mode-custom.png'), animations: 'disabled' })
  step('custom combination selects no chip; screenshot mihomo-ipv6-mode-custom.png')

  // Unreadable YAML disables every chip.
  await desktop.locator('.cm-content').click()
  await desktop.keyboard.press('ControlOrMeta+a')
  await desktop.keyboard.type('a: b: c')
  await desktop.waitForTimeout(500)
  assert.equal(await modes(desktop).locator('[role="radio"]:disabled').count(), 3)
  assert.equal(await desktop.locator('[data-ipv6-mode-description] [role="alert"]').count(), 1)
  step('unreadable YAML disables the IPv6 chips')

  // 4. The same chips as the Agent install dialog, in both themes.
  for (const theme of ['light', 'dark']) {
    const agents = await page(theme, { width: 1440, height: 1000 }, { devices: false })
    await agents.goto(`${base}/agents`)
    await agents.getByRole('button', { name: '生成安装脚本' }).first().click()
    await agents.locator('[role="radiogroup"][aria-label="使用模式"]').waitFor()
    await agents.waitForTimeout(400)
    const theirs = pick(await chipStyles(agents, '[role="radiogroup"][aria-label="使用模式"]'))
    const settings = await page(theme, { width: 1440, height: 1000 })
    await openSettings(settings)
    const ours = pick(await chipStyles(settings, '[role="radiogroup"][aria-label="IPv6 模式"]'))
    sameLook(ours, theirs, `${theme} IPv6 模式`)
    // Both choices sit in the menus of the base settings sheet.
    await settings.locator('[data-base-controller-scope]').click()
    await settings.waitForTimeout(300)
    sameLook(pick(await chipStyles(settings, '[role="radiogroup"][aria-label="控制面板监听"]')), theirs, `${theme} 控制面板监听`)
    await settings.keyboard.press('Escape')
    await settings.locator('[data-base-dns-preset]').click()
    await settings.waitForTimeout(300)
    sameLook(pick(await chipStyles(settings, '[role="radiogroup"][aria-label="DNS 方案"]')), theirs, `${theme} DNS 方案`)
    report.styles[theme] = { agentDialog: theirs, ipv6: ours }
    step(`${theme}: chips match the Agent install dialog`, { on: theirs.on, off: theirs.off })
    await agents.context().close()
    await settings.context().close()
  }

  // 5. Phone, dark: chips wrap onto the next line, no horizontal scroll, 44px targets.
  await api('POST', '/custom-config/mihomo', { config: initial.data.config })
  scene = 'nodes'
  const phone = await page('dark', { width: 390, height: 844 })
  await openSettings(phone)
  await chip(phone, NODES).tap()
  await phone.waitForTimeout(400)
  await phone.getByLabel('IPv6 Fake-IP 网段', { exact: true }).fill('fc00:0:0:2::1/64')
  await phone.getByLabel('IPv6 Fake-IP 网段', { exact: true }).blur()
  await saveDraft(phone, 'nodes')
  await settle(phone)
  const phoneLayout = await checkPhone(phone, '390px')
  assert.ok(new Set(phoneLayout.chips.map(item => item.left)).size > 1 || phoneLayout.chips.every(item => item.right <= 374), 'chips stay inside the gutter')
  await top(phone)
  await quiet(phone)
  await phone.screenshot({ path: join(shots, 'mihomo-ipv6-mode-mobile-dark.png'), animations: 'disabled' })
  step('screenshot mihomo-ipv6-mode-mobile-dark.png')
  // A narrower phone: the long option wraps inside its chip instead of widening the page.
  await phone.setViewportSize({ width: 240, height: 844 })
  await phone.waitForTimeout(400)
  const narrow = await checkPhone(phone, '240px')
  const long = narrow.chips.find(item => item.text === NODES)
  assert.ok(long.height > 44, `the long option wraps: ${JSON.stringify(long)}`)
  await phone.context().close()

  // 6. Dark, 1280px: a saved change not yet published, then everything published and verified.
  const dark = await page('dark', { width: 1280, height: 1000 })
  await openSettings(dark)
  await dark.getByLabel('IPv6 Fake-IP 网段', { exact: true }).fill('fc00:0:0:3::1/64')
  await dark.getByLabel('IPv6 Fake-IP 网段', { exact: true }).blur()
  await saveDraft(dark, 'changed')
  await settle(dark)
  await top(dark)
  await quiet(dark)
  assert.equal(await dark.$eval('[data-ipv6-step="publish"] [data-agent-status-row] [aria-live]', el => el.textContent.trim()), '待发布')
  await sectionShot(dark, 'mihomo-ipv6-pending-after-change', 700)
  scene = 'passed'
  await dark.locator('[data-ipv6-steps-recheck]').click()
  await settle(dark)
  await dark.waitForFunction(() => document.querySelector('[data-ipv6-step="verify"] [data-agent-status-row] [aria-live]')?.textContent.trim() === '已通过', null, { timeout: 30000 })
  await top(dark)
  await quiet(dark)
  await sectionShot(dark, 'mihomo-ipv6-steps-after-router')
  await dark.context().close()

  assert.deepEqual(report.browserErrors, [])
  step('no browser errors')
} finally {
  await writeFile(join(evidence, 'report.json'), JSON.stringify(report, null, 2)).catch(() => {})
  await browser?.close().catch(() => {})
  await vite?.close().catch(() => {})
  backend?.kill()
  await rm(temporary, { recursive: true, force: true }).catch(() => {})
}
