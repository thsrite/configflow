#!/usr/bin/env node
// Upstream router reminder in the real app, against the real VM151 lab router. Test server only:
//   cf-test typecheck   # syncs and links frontend/node_modules
//   rt --no-sync --back doc/img/guide --back .ci-results/upstream-router-pending -- node scripts/tests/upstream_router_pending_browser.mjs
// The backend is this checkout on a private copy of the IPv6 preview data (upstream_router_pending_lab.py).
// Reads VM151 and CT152 only: it saves Mihomo drafts in the copy, never clicks 确认发布 or 保存并发布.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { lstat, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { networkInterfaces } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run this on the test server')
const root = process.cwd()
const frontend = join(root, 'frontend')
const shots = resolve(root, process.env.CF_SHOTS || 'doc/img/guide')
const evidence = resolve(root, '.ci-results/upstream-router-pending')
const apiPort = Number(process.env.CF_LAB_API_PORT || 15881)
const webPort = Number(process.env.CF_LAB_WEB_PORT || 15882)
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const ROUTER = 'router_d10a2f2c9442'   // 测试 ROS (VM151)
const RANGE6 = 'fc00:0:0:1::1/64'
const report = { steps: [], browserErrors: [] }
let backend, vite, browser

async function linkModules() {
  const hash = createHash('sha256').update(await readFile(join(frontend, 'package-lock.json'))).digest('hex').slice(0, 16)
  const cache = `/home/sunbroke/cf-test/node_modules/${hash}/node_modules`
  await lstat(cache)
  await rm(join(frontend, 'node_modules'), { force: true, recursive: false }).catch(() => {})
  await symlink(cache, join(frontend, 'node_modules'))
}

function startBackend() {
  return new Promise((ready, fail) => {
    backend = spawn(python, ['scripts/tests/upstream_router_pending_lab.py', 'serve'], {
      cwd: root, env: { ...process.env, CF_LAB_API_PORT: String(apiPort) }, stdio: ['ignore', 'pipe', 'pipe'] })
    let log = ''
    backend.stderr.on('data', chunk => { log += chunk })
    backend.stdout.on('data', chunk => {
      log += chunk
      const line = String(chunk).split('\n').find(item => item.startsWith('{"ready"'))
      if (line) ready(JSON.parse(line))
    })
    backend.on('exit', code => fail(new Error(`Backend exited ${code}: ${log.slice(-2000)}`)))
  })
}

const base = `http://127.0.0.1:${webPort}`
let token = ''
async function api(method, path, body, profile = 'default') {
  const response = await fetch(`http://127.0.0.1:${apiPort}/api${path}`, {
    method, headers: { 'Content-Type': 'application/json', 'X-ConfigFlow-Profile': profile, ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body) })
  const data = await response.json()
  assert.ok(response.ok, `${method} ${path}: ${response.status} ${data.message || ''}`)
  return data
}
const step = (name, detail = {}) => { report.steps.push({ name, ...detail }); console.log('✓', name) }

async function page(theme, viewport) {
  const context = await browser.newContext({ viewport, deviceScaleFactor: viewport.width < 500 ? 2 : 1, colorScheme: theme,
    hasTouch: viewport.width < 500, isMobile: viewport.width < 500 })
  await context.addInitScript(([value, mode]) => {
    localStorage.setItem('token', value)
    localStorage.setItem('username', 'admin')
    localStorage.setItem('configflow-theme', mode)
    sessionStorage.setItem('configflow.activeProfile', 'default')
  }, [token, theme])
  const tab = await context.newPage()
  tab.on('pageerror', error => report.browserErrors.push(error.message))
  return tab
}

async function openMihomoSettings(tab) {
  await tab.goto(`${base}/generate`)
  await tab.getByRole('button', { name: '基础配置' }).first().click()
  await tab.getByText('Mihomo 基础配置').first().waitFor()
  await tab.locator('[data-settings-publish-bar]').waitFor()
}

// Screenshots wait until the save toast is gone, so it never covers the reminder.
async function quiet(tab) {
  await tab.waitForFunction(() => !document.querySelector('[data-sonner-toast]'), null, { timeout: 15000 })
}

async function saveDraft(tab) {
  await tab.locator('[data-settings-publish-bar]').getByRole('button', { name: '保存草稿' }).click()
  await tab.getByText('基础配置草稿已保存').first().waitFor()
}

try {
  await mkdir(shots, { recursive: true })
  await mkdir(evidence, { recursive: true })
  await linkModules()
  const lab = await startBackend()
  const credentials = JSON.parse(await readFile('/home/sunbroke/previews/configflow-ipv6-readiness/credentials.json', 'utf8'))
  token = (await api('POST', '/auth/login', { username: credentials.username, password: credentials.password })).token
  const { createServer } = await import(pathToFileURL(join(frontend, 'node_modules/vite/dist/node/index.js')))
  vite = await createServer({ root: frontend, configFile: join(frontend, 'vite.config.mts'), logLevel: 'warn',
    server: { host: '127.0.0.1', port: webPort, strictPort: true, proxy: { '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true } } } })
  await vite.listen()
  const { chromium } = await import(pathToFileURL(playwright))
  browser = await chromium.launch({ headless: true })

  // 1. 有待发布（真实 VM151）：改 IPv6 Fake-IP 网段并保存草稿，发布栏出现提醒。
  const desktop = await page('light', { width: 1280, height: 900 })
  await openMihomoSettings(desktop)
  const range = desktop.getByLabel('IPv6 Fake-IP 网段', { exact: true })
  await range.fill(RANGE6)
  await range.blur()
  await saveDraft(desktop)
  const bar = desktop.locator('[data-publish-upstream-routers]')
  const line = bar.locator(`[data-upstream-router="default:${ROUTER}"]`)
  await line.waitFor({ timeout: 60000 })
  const pendingText = (await line.innerText()).replace(/\s+/g, ' ')
  assert.equal(await line.getAttribute('data-state'), 'pending', pendingText)
  assert.match(pendingText, /上游路由「测试 ROS」有 \d+ 项待发布/)
  const states = await api('GET', `/agents/${lab.agent_id}/upstream-routers`)
  const vm151 = states.routers.find(item => item.id === ROUTER)
  assert.ok(vm151.items.some(item => item.key === 'ipv6_fakeip'), JSON.stringify(vm151))
  step('pending: draft save shows the real VM151 changes', { text: pendingText, api: vm151 })
  const footer = desktop.locator('[data-settings-publish-bar]')
  await footer.scrollIntoViewIfNeeded()
  await quiet(desktop)
  const box = await footer.boundingBox()
  await desktop.screenshot({ path: join(shots, 'upstream-router-pending-bar.png'),
    clip: { x: Math.max(0, box.x - 8), y: Math.max(0, box.y - 120), width: Math.min(1280, box.width + 16), height: box.height + 128 } })

  // 设备检查窗口：检查只读取 CT152 和 VM151，同样提醒并列出项目。
  await desktop.getByRole('button', { name: '检查设备 IPv6' }).click()
  const readiness = desktop.locator('[data-mihomo-ipv6-readiness]')
  const checkLine = readiness.locator(`[data-upstream-router="default:${ROUTER}"]`)
  await checkLine.waitFor({ timeout: 90000 })
  const checkText = (await checkLine.innerText()).replace(/\s+/g, ' ')
  assert.match(checkText, /IPv6 Fake-IP 分流路由：待添加/)
  assert.equal(await readiness.getByText('ROS 路由到「上游路由」预览发布').count(), 0)
  step('readiness window lists the items', { text: checkText })
  await checkLine.scrollIntoViewIfNeeded()
  await readiness.screenshot({ path: join(evidence, 'readiness-window.png') })
  await desktop.keyboard.press('Escape')

  // 2. 深链：点「查看并发布」，上游路由页打开差异报告与预览发布；不点确认。
  await line.getByRole('link', { name: '查看并发布' }).click()
  await desktop.waitForURL(url => url.pathname === '/routers')
  const dialog = desktop.getByRole('dialog', { name: /发布到 测试 ROS/ })
  await dialog.waitFor({ timeout: 90000 })
  await desktop.locator('[data-router-report]').first().waitFor()
  assert.equal(new URL(desktop.url()).search, '')
  const dialogText = (await dialog.innerText()).replace(/\s+/g, ' ')
  assert.match(dialogText, /IPv6 Fake-IP 分流路由/)
  step('deep link opens the report and the release preview', { url: desktop.url(), dialog: dialogText.slice(0, 400) })
  await quiet(desktop)
  const frame = await dialog.boundingBox()
  await desktop.screenshot({ path: join(shots, 'upstream-router-deeplink-preview.png'),
    clip: { x: frame.x - 24, y: frame.y - 24, width: frame.width + 48, height: frame.height + 48 } })
  await dialog.getByRole('button', { name: '取消' }).click()

  // 3. 读取失败 + 手机深色：增加一台连不上的路由器，同一台 Mihomo 两行提醒。
  await api('POST', '/routers', { name: '分店 ROS', type: 'routeros', endpoint: 'http://127.0.0.1:9', username: 'admin',
    password: 'lab-only', proxy_gateway_agent_id: lab.agent_id })
  const phone = await page('dark', { width: 390, height: 844 })
  await openMihomoSettings(phone)
  await saveDraft(phone)
  const phoneBar = phone.locator('[data-publish-upstream-routers]')
  await phoneBar.locator('[data-state="unknown"]').waitFor({ timeout: 60000 })
  await phoneBar.locator('[data-state="pending"]').waitFor()
  const unknownText = (await phoneBar.locator('[data-state="unknown"]').innerText()).replace(/\s+/g, ' ')
  assert.match(unknownText, /无法确认上游路由「分店 ROS」状态/)
  for (const link of await phoneBar.getByRole('link').all()) {
    const size = await link.boundingBox()
    assert.ok(size.height >= 44, `touch target ${size.height}`)
  }
  const overflow = await phone.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  assert.ok(overflow <= 0, `horizontal overflow ${overflow}`)
  step('unknown: unreachable router is not synced (phone, dark)', { text: unknownText })
  const phoneFooter = phone.locator('[data-settings-publish-bar]')
  await phoneFooter.scrollIntoViewIfNeeded()
  await quiet(phone)
  const phoneBox = await phoneFooter.boundingBox()
  await phone.screenshot({ path: join(shots, 'upstream-router-pending-mobile-dark.png'),
    clip: { x: 0, y: Math.max(0, phoneBox.y - 60), width: 390, height: Math.min(844 - Math.max(0, phoneBox.y - 60), phoneBox.height + 60) } })

  // 4. 已同步：Mihomo 只被一台已同步的路由器使用，什么都不显示。
  const routers = (await api('GET', '/routers')).routers
  await api('PUT', `/routers/${ROUTER}`, { proxy_gateway_agent_id: '' })
  await api('DELETE', `/routers/${routers.find(item => item.name === '分店 ROS').id}`)
  await api('POST', '/routers', { name: '同步示例', type: 'ikuai', endpoint: 'http://synced.lab.invalid', username: 'admin',
    password: 'lab-only', proxy_gateway_agent_id: lab.agent_id })
  await openMihomoSettings(desktop)
  await saveDraft(desktop)
  let synced
  for (let attempt = 0; attempt < 40 && !synced; attempt++) {
    const data = await api('GET', `/agents/${lab.agent_id}/upstream-routers`)
    if (data.routers.every(item => item.state === 'synced')) synced = data
    else await new Promise(done => setTimeout(done, 500))
  }
  assert.ok(synced, 'synchronised state')
  await desktop.waitForTimeout(1500)
  assert.equal(await desktop.locator('[data-publish-upstream-routers] [data-upstream-router-pending]').count(), 0)
  step('synced: nothing shown', { api: synced.routers })
  await desktop.locator('[data-settings-publish-bar]').screenshot({ path: join(evidence, 'synced-bar.png') })

  // 5. 另一空间也关联同一 Mihomo：Default 的步骤和发布栏仍只核对 Default。
  await api('POST', '/profiles', { id: 'lab-shop', name: '测试空间' })
  await api('POST', '/routers', { name: '同步示例', type: 'routeros', endpoint: 'http://127.0.0.1:9', username: 'admin',
    password: 'lab-only', proxy_gateway_agent_id: lab.agent_id }, 'lab-shop')
  const unscoped = await api('GET', `/agents/${lab.agent_id}/upstream-routers`)
  assert.ok(unscoped.routers.some(item => item.profile_id === 'lab-shop' && item.state !== 'synced'))
  const requestedScopes = []
  desktop.on('request', request => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/upstream-routers')) requestedScopes.push(url.searchParams.get('profile'))
  })
  await openMihomoSettings(desktop)
  await saveDraft(desktop)
  const upstreamStep = desktop.locator('[data-ipv6-step="upstream"]')
  await upstreamStep.getByText('已同步', { exact: true }).waitFor({ timeout: 60000 })
  assert.equal(await desktop.locator('[data-publish-upstream-routers] [data-upstream-router-pending]').count(), 0)
  assert.equal(await desktop.locator('[data-ipv6-step-upstream] a').count(), 0)
  assert.ok(requestedScopes.length > 0 && requestedScopes.every(profile => profile === 'default'))
  const scoped = await api('GET', `/agents/${lab.agent_id}/upstream-routers?profile=default`)
  assert.ok(scoped.routers.length > 0 && scoped.routers.every(item => item.profile_id === 'default' && item.state === 'synced'))
  const active = await desktop.evaluate(() => sessionStorage.getItem('configflow.activeProfile'))
  assert.equal(active, 'default')
  step('Default excludes another profile linked to the same Mihomo', { scopes: requestedScopes, active, api: scoped.routers })
  await quiet(desktop)
  await upstreamStep.scrollIntoViewIfNeeded()
  const scopeBox = await upstreamStep.boundingBox()
  await desktop.screenshot({ path: join(shots, 'upstream-router-profile-scope.png'),
    clip: { x: scopeBox.x - 12, y: scopeBox.y - 12, width: scopeBox.width + 24, height: scopeBox.height + 24 } })
  assert.deepEqual(report.browserErrors, [])
} finally {
  await writeFile(join(evidence, 'report.json'), JSON.stringify(report, null, 2))
  await browser?.close()
  await vite?.close()
  backend?.kill()
}
