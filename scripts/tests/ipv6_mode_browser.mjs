#!/usr/bin/env node
// IPv6 mode, step list and the reused device check in the real app, against VM151 and CT152 (read-only). Test server only:
//   cf-test typecheck   # syncs and links frontend/node_modules
//   rt --no-sync --back doc/img/guide --back .ci-results/ipv6-mode -- node scripts/tests/ipv6_mode_browser.mjs
// The backend is this checkout on a private copy of the IPv6 preview data (upstream_router_pending_lab.py,
// whose read_only() refuses any RouterOS write or Agent operation). Drafts are saved in the copy only;
// 保存并发布, 确认发布, 补齐 and 验证 are never clicked.
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
const evidence = resolve(root, '.ci-results/ipv6-mode')
const work = process.env.CF_LAB_WORK || '/tmp/configflow-ipv6-mode-lab'
const apiPort = Number(process.env.CF_LAB_API_PORT || 15891)
const webPort = Number(process.env.CF_LAB_WEB_PORT || 15892)
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const ROUTER = 'router_d10a2f2c9442'   // 测试 ROS (VM151)
// Not on VM151, so the report has an IPv6 Fake-IP route to add.
const RANGE6 = 'fc00:0:0:2::1/64'
const NODES = 'Mihomo 用 IPv6，客户端走 IPv4'
const CLIENTS = '客户端也走 IPv6'
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
      cwd: root, env: { ...process.env, CF_LAB_API_PORT: String(apiPort), CF_LAB_WORK: work }, stdio: ['ignore', 'pipe', 'pipe'] })
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
const counters = async () => JSON.parse(await readFile(join(work, 'counters.json'), 'utf8'))
const step = (name, detail = {}) => { report.steps.push({ name, ...detail }); console.log('✓', name) }

async function page(theme, viewport) {
  const mobile = viewport.width < 500
  const context = await browser.newContext({ viewport, deviceScaleFactor: mobile ? 2 : 1, colorScheme: theme, hasTouch: mobile, isMobile: mobile })
  await context.addInitScript(([value, mode]) => {
    localStorage.setItem('token', value)
    localStorage.setItem('username', 'admin')
    localStorage.setItem('configflow-theme', mode)
  }, [token, theme])
  const tab = await context.newPage()
  tab.on('pageerror', error => report.browserErrors.push(error.message))
  // Every readiness and reminder answer, to tell a reused result from a new read.
  tab.answers = []
  tab.on('response', async response => {
    const url = new URL(response.url())
    if (!/\/api\/agents\/[^/]+\/(ipv6-readiness|upstream-routers)$/.test(url.pathname)) return
    try {
      const data = await response.json()
      tab.answers.push({ kind: url.pathname.endsWith('upstream-routers') ? 'upstream' : 'readiness',
        refresh: url.searchParams.get('refresh') === '1', cached: data.cached, checked_at: data.checked_at })
    } catch { /* aborted */ }
  })
  return tab
}

async function openSettings(tab) {
  await tab.goto(`${base}/mihomo`)
  await tab.getByRole('button', { name: 'Mihomo 设置' }).click()
  await tab.locator('[data-mihomo-ipv6-settings]').waitFor({ timeout: 30000 })
}

// The steps have read their data: no step in progress and the reminder has answered.
async function settle(tab) {
  await tab.waitForTimeout(300)
  await tab.waitForFunction(() => {
    const steps = document.querySelector('[data-mihomo-ipv6-steps]')
    if (!steps) return true
    const progress = steps.querySelector('[data-tone="progress"]')
    return !progress && !!steps.querySelector('[data-ipv6-step="verify"]')
  }, null, { timeout: 120000 })
  await tab.waitForTimeout(600)
}

async function quiet(tab) {
  await tab.waitForFunction(() => !document.querySelector('[data-sonner-toast]'), null, { timeout: 15000 })
}

const yaml = tab => tab.$eval('.cm-content', el =>
  [...el.querySelectorAll('.cm-line')].map(line => line.textContent).join('\n'))
// Radio chips: each label holds the radio dot and the option name.
const modeName = tab => tab.$$eval('[role="radiogroup"][aria-label="IPv6 模式"] label',
  items => items.filter(item => item.querySelector('[role="radio"]')?.getAttribute('aria-checked') === 'true').map(item => item.textContent.trim()).join(''))
const stepStatus = (tab, id) => tab.$eval(`[data-ipv6-step="${id}"] [data-agent-status-row] [aria-live]`, el => el.textContent.trim()).catch(() => null)
const stepIds = tab => tab.$$eval('[data-ipv6-step]', items => items.filter(item => getComputedStyle(item).display !== 'none').map(item => item.dataset.ipv6Step))
const choose = async (tab, label) => { await tab.getByRole('radio', { name: label, exact: true }).click(); await tab.waitForTimeout(400) }
async function saveDraft(tab) {
  await tab.locator('[data-settings-publish-bar]').getByRole('button', { name: '保存草稿' }).click()
  await tab.getByText('基础配置草稿已保存').first().waitFor()
}
async function top(tab) {
  await tab.$eval('[data-mihomo-ipv6-settings]', el => el.scrollIntoView({ block: 'start' }))
  await tab.waitForTimeout(300)
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

  // 0. 在副本里把 IPv6 设为「关闭」并去掉网段：选择模式时才补齐。
  const saved = await api('GET', '/custom-config/mihomo')
  let base0 = saved.config
  assert.match(base0, /^ipv6: (true|false)/m, 'lab YAML has a top-level ipv6')
  base0 = base0.replace(/^ipv6: (true|false)/m, 'ipv6: false').replace(/^( +)ipv6: true/m, '$1ipv6: false').replace(/^ +fake-ip-range6:.*\n/m, '')
  await api('POST', '/custom-config/mihomo', { config: base0, provider_update_interval: saved.provider_update_interval })
  step('lab draft set to IPv6 off without a range', { yaml: base0.split('\n').filter(line => /ipv6|fake-ip|enhanced|tun|enable/.test(line)) })

  // 1. 选「Mihomo 用 IPv6，客户端走 IPv4」：YAML 补齐网段，说明写完整名称。
  const desktop = await page('light', { width: 1440, height: 1000 })
  await openSettings(desktop)
  assert.equal(await modeName(desktop), '关闭')
  const labels = await desktop.$$eval('[role="radiogroup"][aria-label="IPv6 模式"] label', items => items.map(item => item.textContent.trim()))
  assert.deepEqual(labels, ['关闭', NODES, CLIENTS])
  await choose(desktop, NODES)
  let text = await yaml(desktop)
  assert.match(text, /^ipv6: true/m)
  assert.match(text, /^ +ipv6: false/m)
  assert.match(text, /^ +fake-ip-range6: fc00::1\/64$/m, 'nodes mode fills the Fake-IP range')
  const description = await desktop.locator('[data-ipv6-mode-description]').innerText()
  assert.match(description, /^Mihomo 用 IPv6，客户端走 IPv4 分流：/)
  assert.match(description, /IPv6 Fake-IP 网段和规则集里的 IPv6 网段照常发布到上游路由，切换模式不用再改路由器/)
  assert.equal(await desktop.getByLabel('IPv6 Fake-IP 网段', { exact: true }).inputValue(), 'fc00::1/64')
  assert.equal(await desktop.locator('[data-mihomo-ipv6-settings] [role="status"]').first().innerText(), '已补充 IPv6 Fake-IP 网段 fc00::1/64。')
  step('nodes mode fills fc00::1/64 and names the mode in full', { labels, description })

  // 网段改成 VM151 上没有的，保存草稿：步骤与发布栏读取 VM151 的真实差异。
  const range = desktop.getByLabel('IPv6 Fake-IP 网段', { exact: true })
  await range.fill(RANGE6)
  await range.blur()
  assert.match(await yaml(desktop), new RegExp(`fake-ip-range6: ${RANGE6.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`))
  const before = await counters()
  await saveDraft(desktop)
  // The window stays open after a save, with the success toast.
  await top(desktop)
  await desktop.screenshot({ path: join(shots, 'mihomo-saved-draft-open.png') })
  await settle(desktop)
  await desktop.locator('[data-publish-upstream-routers] [data-state="pending"]').waitFor({ timeout: 90000 })
  await settle(desktop)
  assert.deepEqual(await stepIds(desktop), ['host', 'publish', 'upstream', 'firewall', 'verify'])
  const nodesUpstream = await stepStatus(desktop, 'upstream')
  assert.match(nodesUpstream, /^\d+ 项待处理$/)
  const pending = await api('GET', `/agents/${lab.agent_id}/upstream-routers`)
  const vm151 = pending.routers.find(item => item.id === ROUTER)
  const fake = vm151.items.find(item => item.key === 'ipv6_fakeip')
  assert.ok(fake && fake.status === '待添加', JSON.stringify(vm151))
  const afterSave = await counters()
  assert.ok(afterSave.agent_status > before.agent_status, 'a changed draft is checked again')
  assert.equal(afterSave.routeros_writes + afterSave.agent_writes, 0)
  step('saved nodes draft: VM151 has the IPv6 Fake-IP route to add (dns.ipv6 off)', { upstream: nodesUpstream, api: vm151.items })
  await top(desktop)
  await quiet(desktop)
  await desktop.screenshot({ path: join(shots, 'mihomo-ipv6-mode-nodes.png') })
  const checkedLine = await desktop.locator('[data-ipv6-steps-checked]').innerText()
  assert.match(checkedLine, /^设备(刚刚检查|检查于 \d+ 分钟前)，上游路由(刚刚读取|读取于 \d+ 分钟前)。路由器或主机上的手动修改不会自动发现，请点「重新检查」。$/)
  // No DNS IPv6: the unused Fake-IP route is not a failure.
  const fakeCheck = (await api('GET', `/agents/${lab.agent_id}/ipv6-readiness?profile_id=default`)).routers
    .flatMap(item => item.checks).find(check => check.id === 'fake_ipv6')
  assert.ok(fakeCheck && fakeCheck.status !== 'error' && /DNS IPv6 关闭/.test(fakeCheck.message), JSON.stringify(fakeCheck))
  step('device check does not fault the unused route', { checked: checkedLine, fake_ipv6: fakeCheck })

  // 2. 再次打开设置：直接显示上次结果，不读取设备和路由器。
  const reopened = await page('light', { width: 1440, height: 1000 })
  const quietBefore = await counters()
  await openSettings(reopened)
  await settle(reopened)
  await reopened.locator('[data-ipv6-steps-checked]').waitFor()
  const quietAfter = await counters()
  assert.equal(quietAfter.agent_status, quietBefore.agent_status, 'reopening does not contact the device')
  assert.equal(quietAfter.routeros_reads, quietBefore.routeros_reads, 'reopening does not read the routers')
  const answers = [...reopened.answers]
  assert.ok(answers.length >= 2 && answers.every(item => item.cached === true && !item.refresh), JSON.stringify(answers))
  assert.equal(await stepStatus(reopened, 'upstream'), nodesUpstream)
  step('reopened settings show the last results without reading again', { answers, counters: quietAfter })
  // 「重新检查」 reads both again.
  await reopened.locator('[data-ipv6-steps-recheck]').click()
  await settle(reopened)
  const forced = await counters()
  assert.ok(forced.agent_status > quietAfter.agent_status && forced.routeros_reads > quietAfter.routeros_reads)
  assert.ok(reopened.answers.slice(answers.length).some(item => item.kind === 'readiness' && item.refresh && item.cached === false))
  step('重新检查 checks the device and reads the routers again', { counters: forced })
  await reopened.context().close()

  // 3. 上游路由差异报告：IPv6 Fake-IP 分流路由待添加（DNS IPv6 关闭）。
  const routersTab = await page('light', { width: 1440, height: 1000 })
  await routersTab.goto(`${base}/routers?router=${ROUTER}&profile=default`)
  const card = routersTab.locator('[data-router-ipv6-fakeip]')
  await card.waitFor({ timeout: 90000 })
  const cardText = (await card.innerText()).replace(/\s+/g, ' ')
  assert.match(cardText, /IPv6 Fake-IP 分流路由 待添加/)
  assert.match(cardText, /fc00:0:0:2::\/64/)
  step('router report: IPv6 Fake-IP route to add while DNS IPv6 is off', { card: cardText.slice(0, 300) })
  await quiet(routersTab)
  await card.screenshot({ path: join(shots, 'mihomo-ipv6-nodes-fakeip-pending.png') })
  await routersTab.context().close()

  // 4. 切到「客户端也走 IPv6」：同一条路由，上游路由待处理项不变。
  await choose(desktop, CLIENTS)
  text = await yaml(desktop)
  assert.match(text, /^ +ipv6: true/m)
  assert.equal(text.match(/fake-ip-range6:/g).length, 1)
  await saveDraft(desktop)
  await settle(desktop)
  await desktop.waitForFunction(() => /项待处理|已同步/.test(document.querySelector('[data-ipv6-step="upstream"] [aria-live]')?.textContent || ''), null, { timeout: 90000 })
  await settle(desktop)
  const clientsUpstream = await stepStatus(desktop, 'upstream')
  assert.equal(clientsUpstream, nodesUpstream, 'switching modes leaves the router work unchanged')
  const clientsPending = (await api('GET', `/agents/${lab.agent_id}/upstream-routers`)).routers.find(item => item.id === ROUTER)
  assert.deepEqual(clientsPending.items, vm151.items)
  step('clients mode: same route, same router work', { upstream: clientsUpstream })
  await top(desktop)
  await quiet(desktop)
  await desktop.screenshot({ path: join(shots, 'mihomo-ipv6-mode-steps.png') })

  // 5. 设备检查窗口：沿用上次结果并注明时间。
  await desktop.getByRole('button', { name: '检查设备 IPv6' }).click()
  const dialog = desktop.locator('[data-mihomo-ipv6-dialog]')
  await dialog.locator('[data-ipv6-readiness-checked]').waitFor({ timeout: 90000 })
  await desktop.waitForTimeout(800)
  const dialogChecked = await dialog.locator('[data-ipv6-readiness-checked]').innerText()
  assert.match(dialogChecked, /^(刚刚检查|检查于 )/)
  step('device check window shows the reused result with its time', { checked: dialogChecked })
  await dialog.screenshot({ path: join(shots, 'mihomo-ipv6-three-results.png') })
  await desktop.getByRole('button', { name: '返回设置' }).click()
  await settle(desktop)

  // 6. 手写不属于任何模式的组合：自定义。
  const line = desktop.locator('.cm-line', { hasText: /^ipv6: true$/ }).first()
  await line.click()
  await desktop.keyboard.press('End')
  for (let index = 0; index < 4; index++) await desktop.keyboard.press('Backspace')
  await desktop.keyboard.type('false')
  await desktop.waitForTimeout(500)
  assert.equal(await modeName(desktop), '')
  assert.match(await desktop.locator('[data-ipv6-mode-custom]').innerText(), /自定义/)
  await top(desktop)
  await desktop.screenshot({ path: join(shots, 'mihomo-ipv6-mode-custom.png') })
  step('handwritten combination shows 自定义')

  // 7. 手机深色：长选项折行，无横向滚动，触控目标 44px。
  const phone = await page('dark', { width: 390, height: 844 })
  await openSettings(phone)
  await choose(phone, NODES)
  await saveDraft(phone)
  await settle(phone)
  const wrap = await phone.$$eval('[role="radiogroup"][aria-label="IPv6 模式"] label', items => items.map(el => ({
    text: el.textContent.trim(), height: Math.round(el.getBoundingClientRect().height), width: Math.round(el.getBoundingClientRect().width) }))
    .sort((a, b) => b.text.length - a.text.length)[0])
  assert.equal(wrap.text, NODES)
  assert.ok(wrap.height >= 44, `touch target ${wrap.height}`)
  const overflow = await phone.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  assert.ok(overflow <= 0, `horizontal overflow ${overflow}`)
  const group = await phone.$eval('[role="radiogroup"][aria-label="IPv6 模式"]', el => el.getBoundingClientRect().right)
  assert.ok(group <= 390, `radio chips right edge ${group}`)
  const small = await phone.$$eval('[data-mihomo-ipv6-settings] button, [data-mihomo-ipv6-settings] a', items => items
    .filter(item => getComputedStyle(item).display !== 'none' && item.getBoundingClientRect().height > 0 && item.getBoundingClientRect().height < 44
      && !item.hasAttribute('data-field-help-trigger') && !item.closest('[data-agent-status-row]') && !/复制/.test(item.textContent))
    .map(item => `${item.textContent.trim() || item.getAttribute('aria-label')}:${Math.round(item.getBoundingClientRect().height)}`))
  assert.deepEqual(small, [])
  step('phone: the long option wraps, no horizontal scroll', { wrap, overflow })
  await top(phone)
  await quiet(phone)
  await phone.screenshot({ path: join(shots, 'mihomo-ipv6-mode-mobile-dark.png') })

  const final = await counters()
  assert.equal(final.routeros_writes + final.agent_writes, 0, 'nothing was written to VM151 or CT152')
  step('no RouterOS write or Agent operation was attempted', { counters: final })
  assert.deepEqual(report.browserErrors, [])
} finally {
  await writeFile(join(evidence, 'report.json'), JSON.stringify(report, null, 2))
  await browser?.close()
  await vite?.close()
  backend?.kill()
}
