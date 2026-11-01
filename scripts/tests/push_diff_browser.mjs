#!/usr/bin/env node
// 「待推送的变更」 in the real app, against a private ConfigFlow (no API mocks): the PVE lab one of
// push_diff_lab.py, or the device-less one of push_diff_local.py (CF_PUSHDIFF_WORK=/tmp/cf-pushdiff-local
// CF_PUSHDIFF_AGENT='实验 Mihomo'). Test server only, after `cf-test typecheck` linked frontend/node_modules:
//   rt --no-sync --back doc/img/guide --back .ci-results/push-diff -- node scripts/tests/push_diff_browser.mjs changed
//   ... push_diff_browser.mjs nobaseline | error | synced   (lab data only)
// Reads only: never clicks 推送 (the lab script publishes). `error` deletes the old-version record through the API.
// `changed` checks values are shown by default, that 隐藏敏感信息 leaves no credential in the page (tables,
// details, raw diff) and that switching it off shows them again, on a light desktop and a dark phone; with the
// local data it also expands the subscription and rule set rows (nodes in tables of 节点 | 变更项 | 变更前 | 变更后:
// added and removed with type and server:port, IPv6 included, modified field by field with nested paths, a repeated
// name; phone records of 「变更项：变更前 → 变更后」; rules added and removed as a list), loads every page of the large
// ones (+3000 / −1500 rules, +300 nodes) inside their own scroll area, searches them (hits and misses, node matches
// in the same table), checks that credentials on pages loaded while 隐藏敏感信息 is on never reach the page, and
// changes the configuration under an open dialog to see 对比结果已更新. 表 2 is grouped by top-level key (基础设置,
// DNS with a proxy-server-nameserver-policy block of three domains, Hosts with three names), long names break only
// after . - /, and a group of more than 20 rows (22 more hosts, added for a moment through the API) starts folded.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { networkInterfaces } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run this on the test server')
const scene = process.argv[2]
const root = process.cwd()
const frontend = join(root, 'frontend')
const shots = resolve(root, 'doc/img/guide')
const evidence = resolve(root, '.ci-results/push-diff')
const apiPort = 15951
const webPort = 15955
const playwright = process.env.CF_PLAYWRIGHT_MODULE
  || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs'
const WORK = process.env.CF_PUSHDIFF_WORK || '/tmp/cf-pushdiff-lab'
const LAB = process.env.CF_PUSHDIFF_AGENT || '实验 Mihomo（CT152）'
const report = { scene, work: WORK, steps: [], browserErrors: [] }
let vite, browser

const credentials = JSON.parse(await readFile(join(WORK, 'credentials.json'), 'utf8'))
// Lab credentials, and ConfigFlow's own access token in the subscription pull URLs.
const SECRETS = ['lab-manual-pass-before-7c1e', 'lab-manual-pass-after-93ab', 'lab-controller-secret-5d20', credentials.config_token]
// The subscription node's password before and after (push_diff_local.py only).
const NODE_SECRETS = ['lab-node-pass-before-41f2', 'lab-node-pass-after-8d3c']
// 40 more nodes changing their password: 19 on the first page, the rest on the next one.
const BULK_SECRETS = Array.from({ length: 40 }, (_, index) => String(index + 1).padStart(2, '0'))
  .flatMap(index => [`lab-bulk-pass-before-${index}`, `lab-bulk-pass-after-${index}`])
const LOCAL = WORK.endsWith('cf-pushdiff-local')
if (LOCAL) SECRETS.push(...NODE_SECRETS)
const login = await fetch(`http://127.0.0.1:${apiPort}/api/auth/login`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: credentials.username, password: credentials.password }) })
const { token } = await login.json()
assert.ok(token, 'login')

async function page(theme, viewport) {
  const phone = viewport.width < 500
  const context = await browser.newContext({ viewport, deviceScaleFactor: phone ? 2 : 1, colorScheme: theme, hasTouch: phone, isMobile: phone,
    locale: 'zh-CN', timezoneId: 'Asia/Shanghai' })
  await context.addInitScript(([value, mode]) => {
    localStorage.setItem('token', value)
    localStorage.setItem('username', 'admin')
    localStorage.setItem('configflow-theme', mode)
  }, [token, theme])
  const tab = await context.newPage()
  tab.on('pageerror', error => report.browserErrors.push(error.message))
  return tab
}

/** The card's sync label once the server has compared (it shows 正在核对配置 meanwhile). */
async function syncButton(tab, name, label) {
  const deadline = Date.now() + 150000
  await tab.goto(`http://127.0.0.1:${webPort}/agents`)
  for (;;) {
    const card = tab.locator('.cf-list-in').filter({ hasText: name })
    await card.locator('footer').waitFor({ timeout: 60000 })
    // The list renders before content checks answer; give the label a moment to settle.
    await tab.waitForTimeout(1500)
    const button = card.locator('[data-sync-diff]')
    if (await button.count() && (!label || (await button.innerText()).includes(label))) return { card, button }
    if (Date.now() >= deadline) {
      await tab.screenshot({ path: join(evidence, `missing-label-${scene}.png`), fullPage: true })
      report.steps.push({ name: 'cards when the label was missing', cards: (await tab.locator('.cf-list-in').allInnerTexts()).map(text => text.replace(/\s+/g, ' ').slice(0, 300)) })
      assert.fail(`no ${label || 'clickable'} label on ${name}`)
    }
    await tab.waitForTimeout(3000)
    await tab.reload()
  }
}

async function openDialog(tab, name, label, ready) {
  const { button } = await syncButton(tab, name, label)
  await button.scrollIntoViewIfNeeded()
  const box = await button.boundingBox()
  report.steps.push({ name: `${name}: sync label size`, width: box?.width, height: box?.height })
  await button.focus()
  await tab.keyboard.press('Enter')  // a real button: keyboard opens it
  const dialog = tab.locator('[data-push-diff]')
  await dialog.locator(ready).waitFor({ timeout: 180000 })
  await tab.mouse.move(0, 0)
  await tab.waitForTimeout(400)
  return dialog
}

async function noOverflow(tab, dialog, name) {
  const page = await tab.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  const body = await dialog.evaluate(element => {
    const scroller = element.querySelector('.overflow-y-auto')
    return scroller.scrollWidth - scroller.clientWidth
  })
  const tables = await dialog.evaluate(element => [...element.querySelectorAll('.cf-table-wrap')]
    .map(wrap => wrap.scrollWidth - wrap.clientWidth))
  // The expanded details, the node tables and every laid-out cell in their bodies (the phone's header is
  // visually hidden in a 1px box): nothing wider than its box.
  const { inner, widest } = await dialog.evaluate(element => {
    const boxes = [...element.querySelectorAll('[data-details-scroll], [data-node-table]')]
    const cells = [...element.querySelectorAll('[data-node-table] > tbody th, [data-node-table] > tbody td')]
      .filter(cell => ['table-cell', 'block'].includes(getComputedStyle(cell).display))
    let inner = 0, widest = ''
    for (const node of [...boxes, ...cells]) {
      const over = node.scrollWidth - node.clientWidth
      if (over > inner) [inner, widest] = [over, `${node.tagName} ${node.textContent.slice(0, 60)}`]
    }
    return { inner, widest }
  })
  report.steps.push({ name: `${name}: no horizontal overflow`, page, body, tables, inner, widest })
  assert.ok(page <= 0 && body <= 0 && inner <= 0 && tables.every(value => value <= 0),
    `horizontal overflow ${JSON.stringify({ page, body, tables, inner, widest })}`)
}

async function assertNoSecrets(tab) {
  const html = await tab.content()
  for (const secret of SECRETS) assert.ok(!html.includes(secret), `secret visible: ${secret.slice(0, 12)}…`)
}

async function assertSecretsShown(tab, secrets = SECRETS) {
  const html = await tab.content()
  for (const secret of secrets) assert.ok(html.includes(secret), `value not shown: ${secret.slice(0, 12)}…`)
}

/** 隐藏敏感信息: a real switch with a 44px target, off on every opening. */
async function setHidden(tab, dialog, on, name) {
  const control = dialog.locator('[data-push-diff-hide]')
  const box = await control.boundingBox()
  const toggle = control.getByRole('switch')
  report.steps.push({ name: `${name}: switch ${on ? 'on' : 'off'}`, height: box?.height, before: await toggle.getAttribute('aria-checked') })
  assert.ok(box && box.height >= 44, `switch target ${box?.height}px high`)
  await control.getByText('隐藏敏感信息').click()  // the label text toggles it too
  assert.equal(await toggle.getAttribute('aria-checked'), String(on))
  await tab.waitForTimeout(150)
}

async function openRaw(dialog) {
  const button = dialog.getByRole('button', { name: '原始差异' })
  if ((await button.getAttribute('aria-expanded')) !== 'true') await button.click()
  await dialog.locator('#push-diff-raw-text').waitFor()
}

async function expand(dialog, key) {
  const row = dialog.locator(`[data-key="${key}"]`)
  const toggle = row.getByRole('button', { name: '展开明细' })
  if (await toggle.count()) await toggle.click()
}

/** 展开明细 of a file row in table 1; returns the detail row right below it. */
async function expandFile(dialog, path, name) {
  const row = dialog.locator(`[data-file="${path}"]`)
  const toggle = row.getByRole('button', { name: '展开明细' })
  await toggle.scrollIntoViewIfNeeded()
  const box = await toggle.boundingBox()
  report.steps.push({ name: `${name}: 展开明细 of ${path}`, width: box?.width, height: box?.height })
  await toggle.click()
  assert.equal(await row.getByRole('button', { name: '收起明细' }).getAttribute('aria-expanded'), 'true')
  const detail = row.locator('xpath=following-sibling::tr[1]')
  await detail.locator('[data-file-details]').waitFor()
  return { detail, box }
}

const flat = async locator => (await locator.innerText()).replace(/\s+/g, ' ')

const group = (detail, change) => detail.locator(`[data-group="${change}"]`)
// Rules are list items, nodes are table row groups (one tbody per node).
const ENTRIES = ':scope > ul > li, :scope > table > tbody'
const entryCount = async (detail, change) => group(detail, change).locator(ENTRIES).count()

/** A group's node table as shown: header cells and, per node (tbody), each row's visible cells. */
async function nodeTable(detail, change) {
  return group(detail, change).locator('[data-node-table]').evaluate(table => {
    const text = cell => cell.innerText.replace(/\s+/g, ' ').trim()
    return { head: [...table.tHead.rows[0].cells].map(text),
      nodes: [...table.tBodies].map(body => [...body.rows].map(row => [...row.cells].map(text))) }
  })
}

/** Desktop geometry of a node table: column widths, and the first node's name spanning its rows. */
async function nodeColumns(detail, change) {
  return group(detail, change).locator('[data-node-table]').evaluate(table => {
    const name = table.tBodies[0].rows[0].cells[0]
    return { layout: getComputedStyle(table).tableLayout, width: Math.round(table.getBoundingClientRect().width),
      widths: [...table.tHead.rows[0].cells].map(cell => Math.round(cell.getBoundingClientRect().width)),
      rows: table.tBodies[0].rows.length, nameHeight: Math.round(name.getBoundingClientRect().height),
      nodeHeight: Math.round(table.tBodies[0].getBoundingClientRect().height) }
  })
}

/** Phone records of a node table: the hidden header, and per node its lines (name, then 变更项：变更前 → 变更后). */
async function nodeRecords(detail, change, limit = 3) {
  return group(detail, change).locator('[data-node-table]').evaluate((table, count) => ({
    head: Math.round(table.tHead.getBoundingClientRect().height),
    display: getComputedStyle(table.tBodies[0]).display,
    nodes: table.tBodies.length,
    records: [...table.tBodies].slice(0, count).map(body => body.innerText.split('\n').map(line => line.replace(/\s+/g, ' ').trim()).filter(Boolean)),
  }), limit)
}

/** 表 2 as shown: per group its title button (text, expanded), then block titles (▸) and rows (visible cells). */
async function configOutline(dialog) {
  return dialog.locator('[data-push-diff-config]').evaluate(table => [...table.tBodies].map(body => {
    const text = node => node.innerText.replace(/\s+/g, ' ').trim()
    const toggle = body.querySelector('[data-group-toggle]')
    return { title: text(toggle), expanded: toggle.getAttribute('aria-expanded'),
      lines: [...body.querySelectorAll(':scope > tr[data-key], :scope > tr[data-config-block]')].map(row => row.matches('[data-config-block]')
        ? `▸ ${text(row)}` : [...row.cells].filter(cell => getComputedStyle(cell).display !== 'none').map(text)) }
  }))
}

/** Long names break only at a <wbr> (after . - /): no text segment of a label or block name spans two lines. */
async function breaksOnlyAtSeparators(dialog) {
  return dialog.evaluate(element => {
    const broken = [], wrapped = []
    for (const holder of element.querySelectorAll('[data-config-label], [data-config-block] .font-mono, [data-node-key] .font-mono')) {
      const walker = document.createTreeWalker(holder, NodeFilter.SHOW_TEXT)
      const lines = new Set()
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!node.textContent.trim() || node.parentElement.closest('[aria-hidden="true"]')) continue
        const range = document.createRange()
        range.selectNodeContents(node)
        const rects = [...range.getClientRects()].filter(rect => rect.width > 0)
        if (rects.length > 1) broken.push(node.textContent)
        rects.forEach(rect => lines.add(Math.round(rect.top)))
      }
      if (lines.size > 1) wrapped.push(holder.textContent.trim())
    }
    return { broken, wrapped }
  })
}

/** Phone records of 表 2: 配置项：原值 → 新值 as one line of text, the change chip at the top right. */
async function configRecords(dialog, group) {
  return dialog.locator(`[data-config-group="${group}"]`).evaluate(body => [...body.querySelectorAll(':scope > tr[data-key]')].map(row => {
    const chip = row.querySelector('.push-diff-change .chip').getBoundingClientRect()
    const box = row.getBoundingClientRect()
    return { line: [...row.cells].filter(cell => getComputedStyle(cell).display !== 'none' && !cell.classList.contains('push-diff-change'))
      .map(cell => cell.innerText).join('').replace(/\s+/g, ' ').trim(),
    chipRight: Math.round(box.right - chip.right), chipTop: Math.round(chip.top - box.top), nested: row.classList.contains('is-nested') }
  }))
}

const POLICY = 'proxy-server-nameserver-policy'
// Long enough to wrap on a 375px phone (at a separator), short enough to fit on a desktop.
const POLICY_DOMAINS = ['node-1.backbone-edges.example.net', 'node-2.backbone-edges.example.net', 'node-3.backbone-edges.example.net']
const HOSTS = [['printer.lab.invalid', '198.51.100.30'], ['nas.lab.invalid', '198.51.100.31'], ['camera-01.lab.invalid', '198.51.100.32']]

/** 22 more hosts for a moment (reverted in revertHosts): the Hosts group passes 20 rows and starts folded. */
let customBefore = null
async function addHosts() {
  const current = await ruleLibrary('GET', '/custom-config/mihomo')
  customBefore = { config: current.config, provider_update_interval: current.provider_update_interval }
  const extra = Array.from({ length: 22 }, (_, index) => `  extra-${String(index + 1).padStart(2, '0')}.lab.invalid: 198.51.100.${100 + index}\n`).join('')
  assert.match(current.config, /hosts:\n(  .*\n)+$/, 'hosts is the last block of the custom configuration')
  await ruleLibrary('POST', '/custom-config/mihomo', { ...customBefore, config: current.config + extra })
}
async function revertHosts() {
  if (!customBefore) return
  await ruleLibrary('POST', '/custom-config/mihomo', customBefore)
  customBefore = null
}

/** 加载更多 once and wait for the page to be added. */
async function loadMore(detail, change) {
  const section = group(detail, change)
  const before = await entryCount(detail, change)
  await section.locator('[data-load-more]').click()
  await detail.page().waitForFunction(([element, count, entries]) => element.querySelectorAll(entries).length > count
    && !element.querySelector('[data-load-more][disabled]'), [await section.elementHandle(), before, ENTRIES])
}

/** 加载更多 in one group until it is gone; returns the number of clicks. */
async function loadAll(detail, change) {
  let clicks = 0
  while (await group(detail, change).locator('[data-load-more]').count()) {
    await loadMore(detail, change)
    clicks += 1
  }
  return clicks
}

/** Type into the details' search box and wait for the server's answers (one request per group). */
async function searchFor(detail, text) {
  const tab = detail.page()
  const answered = tab.waitForResponse(response => response.url().includes('/push-diff/entries')
    && new URL(response.url()).searchParams.get('q') === text)
  await detail.locator('[data-detail-search]').fill(text)
  await answered
  await tab.waitForFunction(element => /匹配/.test(element.textContent), await detail.locator('[data-search-summary]').elementHandle())
  return flat(detail.locator('[data-search-summary]'))
}

/** The details' own scroll area: bounded height, scrolled by the wheel without moving the dialog. */
async function innerScroll(tab, dialog, detail, name, limit) {
  const scroller = detail.locator('[data-details-scroll]')
  await scroller.evaluate(element => { element.scrollTop = 0 })
  const box = await scroller.boundingBox()
  const before = await scroller.evaluate(element => ({ top: element.scrollTop, height: element.clientHeight, content: element.scrollHeight }))
  const outer = () => dialog.evaluate(element => element.querySelector('.overflow-y-auto').scrollTop)
  const outerBefore = await outer()
  await tab.mouse.move(box.x + box.width / 2, box.y + Math.min(box.height / 2, 120))
  await tab.mouse.wheel(0, 600)
  await tab.waitForTimeout(300)
  const after = await scroller.evaluate(element => element.scrollTop)
  const outerAfter = await outer()
  report.steps.push({ name: `${name}: inner scroll`, ...before, after, outerBefore, outerAfter, limit })
  assert.ok(before.height <= limit + 1, `details area ${before.height}px high, limit ${limit}`)
  assert.ok(before.content > before.height, 'details area scrolls')
  assert.ok(after > before.top, 'the wheel scrolls the details area')
  assert.equal(outerAfter, outerBefore, 'the dialog stays where it is')
  await tab.mouse.move(0, 0)
}

async function collapseFile(dialog, path) {
  const toggle = dialog.locator(`[data-file="${path}"]`).getByRole('button', { name: '收起明细' })
  if (await toggle.count()) await toggle.click()
}

/** Change the small rule set while a dialog is open (reverted in revertBump). */
let bumped = null
async function ruleLibrary(method, path, body) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/api${path}`, { method, headers: { 'Content-Type': 'application/json',
    Authorization: `Bearer ${token}` }, body: body && JSON.stringify(body) })
  assert.ok(response.ok, `${method} ${path}: ${response.status}`)
  return response.json()
}
async function bump() {
  const rule = (await ruleLibrary('GET', '/rule-library')).find(item => item.id === 'lab-direct')
  delete rule.has_ip_part
  bumped = rule
  await ruleLibrary('PUT', '/rule-library/lab-direct', { ...rule, content: `${rule.content}\n+.bump.lab.invalid` })
}
async function revertBump() {
  if (!bumped) return
  await ruleLibrary('PUT', '/rule-library/lab-direct', bumped)
  bumped = null
}

/** Scroll the dialog body (not the dialog itself) so ``selector`` sits at its top. */
async function scrollToTop(dialog, selector) {
  await dialog.evaluate((element, target) => {
    const scroller = element.querySelector('.overflow-y-auto')
    const node = element.querySelector(target)
    scroller.scrollTop += node.getBoundingClientRect().top - scroller.getBoundingClientRect().top - 8
  }, selector)
}

async function shot(tab, dialog, file) {
  await tab.locator('[data-sonner-toast]').first().waitFor({ state: 'detached', timeout: 15000 }).catch(() => {})
  await tab.evaluate(() => document.activeElement?.blur())
  await dialog.evaluate(element => { element.scrollTop = 0 })  // header and footer stay in the picture
  await tab.mouse.move(0, 0)
  await tab.waitForTimeout(250)
  await dialog.screenshot({ path: file })
}

try {
  await mkdir(shots, { recursive: true })
  await mkdir(evidence, { recursive: true })
  const { createServer } = await import(pathToFileURL(join(frontend, 'node_modules/vite/dist/node/index.js')))
  vite = await createServer({ root: frontend, configFile: join(frontend, 'vite.config.mts'), logLevel: 'warn',
    server: { host: '127.0.0.1', port: webPort, strictPort: true, proxy: { '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true } } } })
  await vite.listen()
  const { chromium } = await import(pathToFileURL(playwright))
  browser = await chromium.launch({ headless: true })

  if (scene === 'changed') {
    const desktop = await page('light', { width: 1280, height: 900 })
    const dialog = await openDialog(desktop, LAB, '有变更，待推送', '[data-push-diff-changed]')
    const text = (await dialog.innerText()).replace(/\s+/g, ' ')
    report.steps.push({ name: 'changed (desktop, light)', text: text.slice(0, 2400) })
    assert.match(text, /主配置.*修改/)
    assert.match(text, LOCAL ? /实验机场.*订阅.*修改.*节点 \+2 \/ −1 · 修改 41 · 共 44 个/ : /实验机场.*订阅.*修改.*节点 \+1 \/ −0/)
    assert.match(text, /实验直连.*规则集.*修改.*规则 \+2 \/ −1/)
    if (LOCAL) {
      assert.match(text, /备用机场.*订阅.*新增.*节点 \+300 \/ −0/)
      assert.match(text, /实验大规则集.*规则集.*修改.*规则 \+3000 \/ −1500/)
    }
    assert.match(text, /printer\.lab\.invalid 新增 — 198\.51\.100\.30/)
    assert.match(text, /secret 新增 — lab-controller-secret-5d20/)  // values as they are by default
    // A subscription added to the group: its pull URL carries ConfigFlow's access token (push_diff_local.py).
    const backup = dialog.locator('[data-key="proxy-providers.备用机场"]')
    const pullToken = await backup.count() > 0
    if (pullToken) assert.ok((await backup.innerText()).includes(`token=${credentials.config_token}`), 'pull token shown')
    const shown = pullToken ? SECRETS : SECRETS.slice(0, 3)
    await expand(dialog, 'proxies')
    const proxies = (await dialog.locator('[data-key="proxies"] + tr').innerText()).replace(/\s+/g, ' ')
    assert.match(proxies, /~ 手动 SS password：lab-manual-pass-before-7c1e → lab-manual-pass-after-93ab/)
    await assertSecretsShown(desktop, shown.filter(secret => !NODE_SECRETS.includes(secret)))
    await scrollToTop(dialog, '[data-push-diff-changed]')  // the window as it opens
    await shot(desktop, dialog, join(shots, 'push-diff-changed.png'))
    if (LOCAL) {
      // 表 2 grouped by top-level key: a title row per group, paths inside the group, a block title for the
      // policy's three domains; every group open (none passes 20 rows).
      await dialog.locator('[data-key="proxies"]').getByRole('button', { name: '收起明细' }).click()  // opened above; closed for the picture
      const outline = await configOutline(dialog)
      report.steps.push({ name: 'table 2 groups (desktop, light)', outline })
      assert.deepEqual(outline.map(group => [group.title, group.expanded]), [
        ['基础设置 新增 2 项 · 修改 1 项', 'true'], ['DNS dns 新增 3 项 · 修改 1 项', 'true'], ['Hosts hosts 新增 3 项', 'true'],
        ['节点 proxies 修改 1 项', 'true'], ['订阅 proxy-providers 新增 1 项', 'true'], ['代理组 proxy-groups 修改 1 项', 'true']])
      assert.deepEqual(outline[0].lines.map(row => row.slice(0, 2)), [['log-level', '修改'], ['unified-delay', '新增'], ['secret', '新增']])
      assert.deepEqual(outline[0].lines[0], ['log-level', '修改', 'info', 'debug'])
      const dns = outline[1].lines
      assert.equal(dns.length, 5)
      assert.match(dns[0].join(' '), /^fake-ip-filter 修改 2 条 3 条 \+1 \/ −0 展开明细$/)
      assert.equal(dns[1], `▸ ${POLICY} · 新增 3 项`)
      assert.deepEqual(dns.slice(2), POLICY_DOMAINS.map(domain => [domain, '新增', '—', '198.51.100.54']))
      assert.deepEqual(outline[2].lines, HOSTS.map(([name, address]) => [name, '新增', '—', address]))
      assert.deepEqual(outline[4].lines.map(row => row[0]), ['备用机场'])
      const breaks = await breaksOnlyAtSeparators(dialog)
      report.steps.push({ name: 'table 2 breaks (desktop, light)', ...breaks })
      assert.deepEqual(breaks.broken, [], `names broken inside a word: ${breaks.broken}`)
      const columns = await dialog.locator('[data-push-diff-config]').evaluate(table => ({ layout: getComputedStyle(table).tableLayout,
        widths: [...table.tHead.rows[0].cells].map(cell => Math.round(cell.getBoundingClientRect().width)) }))
      report.steps.push({ name: 'table 2 columns (desktop, light)', ...columns })
      assert.equal(columns.layout, 'fixed')
      assert.ok(Math.abs(columns.widths[2] - columns.widths[3]) <= 1 && columns.widths[2] >= 96, `原值 / 新值 ${columns.widths}`)
      await scrollToTop(dialog, '#push-diff-config-title')
      await shot(desktop, dialog, join(shots, 'push-diff-config-groups.png'))
      await scrollToTop(dialog, '[data-push-diff-changed]')
      await expand(dialog, 'proxies')
    }

    if (LOCAL) {
      // Table 1 rows expand into nodes and rules, grouped by change, values as they are.
      const { detail: feed } = await expandFile(dialog, 'providers/实验机场.yaml', 'desktop')
      const nodes = await flat(feed)
      report.steps.push({ name: 'subscription details (desktop, light)', text: nodes })
      for (const part of ['新增 2 个节点', '删除 1 个节点', '修改 41 个节点', '加载更多（还剩 21 个）', '有 1 个节点名被多个节点共用']) {
        assert.ok(nodes.includes(part), `subscription details miss: ${part}`)
      }
      // One table per group: 节点 | 变更项 | 变更前 | 变更后. Added and removed nodes mirror each other.
      const added = await nodeTable(feed, 'added')
      assert.deepEqual(added.head, ['节点', '变更项', '变更前', '变更后'])
      assert.deepEqual(added.nodes, [[['C 新增节点', '新增', '—', 'trojan · edge-c.lab.invalid:443']],
        [['B 日本（同名第 2 个）', '新增', '—', 'socks5 · [2001:db8::b]:1080']]])
      assert.deepEqual((await nodeTable(feed, 'removed')).nodes, [[['E 下线', '删除', 'socks5 · [2001:db8::e]:1080', '—']]])
      // A modified node: a row per field (type, server, port first; nested fields by path), its name across them.
      const modified = await nodeTable(feed, 'modified')
      assert.deepEqual(modified.nodes[0], [['A 香港', 'server', 'edge-a.lab.invalid', 'edge-a2.lab.invalid'],
        ['port', '11005', '11008'], ['password', ...NODE_SECRETS], ['plugin-opts.host', 'cdn-a.lab.invalid', 'cdn-b.lab.invalid']])
      assert.equal(modified.nodes.length, 20, 'the first page ends at 20 modified nodes')
      assert.deepEqual(modified.nodes[19], [['F 批量 19', 'password', 'lab-bulk-pass-before-19', 'lab-bulk-pass-after-19']])
      assert.ok(!nodes.includes('F 批量 20'), 'the first page ends at 20 modified nodes')
      assert.ok(!nodes.includes('lab-trojan-pass-c7') && !nodes.includes('lab-node-pass-e'), 'added and removed nodes show type and address only')
      const columns = await nodeColumns(feed, 'modified')
      report.steps.push({ name: 'node table columns (desktop, light)', ...columns, tables: modified.nodes.length })
      assert.equal(columns.layout, 'fixed')
      assert.ok(Math.abs(columns.widths[2] - columns.widths[3]) <= 1, `变更前 / 变更后 widths ${columns.widths}`)
      assert.ok(columns.widths.every(width => width >= 96), `a column under 96px: ${columns.widths}`)
      assert.ok(columns.rows === 4 && Math.abs(columns.nameHeight - columns.nodeHeight) <= 1, 'the name spans its node’s rows')
      // Each group's table has the same columns.
      const addedColumns = await nodeColumns(feed, 'added')
      assert.deepEqual(addedColumns.widths, columns.widths)
      // A removed node is found by its server as well.
      assert.equal(await searchFor(feed, '2001:db8::e'), '匹配 1 个')
      assert.deepEqual((await nodeTable(feed, 'removed')).nodes, [[['E 下线', '删除', 'socks5 · [2001:db8::e]:1080', '—']]])
      await feed.getByRole('button', { name: '清空搜索' }).click()
      assert.equal(await entryCount(feed, 'modified'), 20)
      const { detail: direct } = await expandFile(dialog, 'ruleset/实验直连.list', 'desktop')
      const rules = await flat(direct)
      for (const part of ['新增 2 条规则', '+ +.delta.lab.invalid', '+ +.epsilon.lab.invalid', '删除 1 条规则', '− +.beta.lab.invalid']) {
        assert.ok(rules.includes(part), `rule details miss: ${part}`)
      }
      assert.ok(!rules.includes('未列出'))
      await assertSecretsShown(desktop, shown)
      await scrollToTop(dialog, '#push-diff-files-title')
      await shot(desktop, dialog, join(shots, 'push-diff-file-details.png'))

      // Many changes: 20 of each group first, 50 more per click, inside the expanded area's own scroll.
      const bigPath = 'ruleset/实验大规则集.list'
      const { detail: big } = await expandFile(dialog, bigPath, 'desktop')
      assert.match(await flat(big), /新增 3000 条规则.*加载更多（还剩 2980 条）.*删除 1500 条规则.*加载更多（还剩 1480 条）/)
      assert.equal(await entryCount(big, 'added'), 20)
      await loadMore(big, 'added')
      assert.ok((await flat(group(big, 'added'))).includes('加载更多（还剩 2930 条）'))
      assert.equal(await entryCount(big, 'added'), 70)
      await scrollToTop(dialog, `[data-file="${bigPath}"]`)
      await innerScroll(desktop, dialog, big, 'desktop', 420)
      // The picture: the end of the loaded part with the next 加载更多, inside the area's own scroll.
      await big.locator('[data-details-scroll]').evaluate(element => {
        const button = element.querySelector('[data-group="added"] [data-load-more]')
        element.scrollTop += button.getBoundingClientRect().bottom - element.getBoundingClientRect().bottom + 12
      })
      await shot(desktop, dialog, join(shots, 'push-diff-load-more.png'))
      const started = Date.now()
      const clicks = (await loadAll(big, 'added')) + 1
      const removedClicks = await loadAll(big, 'removed')
      assert.equal(await entryCount(big, 'added'), 3000)
      assert.equal(await entryCount(big, 'removed'), 1500)
      assert.equal(await group(big, 'added').locator('[data-all-loaded]').innerText(), '已全部加载')
      const bigText = await flat(big)
      for (const part of ['+ +.fresh-0000.big.lab.invalid', '+ +.fresh-2999.big.lab.invalid', '− +.site-0000.big.lab.invalid',
        '− +.site-1499.big.lab.invalid']) assert.ok(bigText.includes(part), `all pages: missing ${part}`)
      report.steps.push({ name: 'rules: every page loaded', added: 3000, removed: 1500, clicks, removedClicks, ms: Date.now() - started })
      await innerScroll(desktop, dialog, big, 'desktop, all loaded', 420)
      await noOverflow(desktop, dialog, 'desktop, all pages')

      // Search on the server: hits page like the list, a miss says so, clearing restores the list.
      assert.equal(await searchFor(big, 'FRESH-12'), '匹配 100 条')
      assert.match(await flat(group(big, 'added').locator('p').first()), /新增 3000 条规则 · 匹配 100 条/)
      assert.match(await flat(group(big, 'removed').locator('p').first()), /删除 1500 条规则 · 匹配 0 条/)
      assert.equal(await entryCount(big, 'added'), 50)
      assert.ok((await flat(group(big, 'added'))).includes('加载更多（还剩 50 条）'))
      await scrollToTop(dialog, `[data-file="${bigPath}"]`)
      await big.locator('[data-details-scroll]').evaluate(element => { element.scrollTop = 0 })
      await shot(desktop, dialog, join(shots, 'push-diff-search.png'))
      await loadAll(big, 'added')
      assert.equal(await entryCount(big, 'added'), 100)
      const hits = await group(big, 'added').locator(':scope > ul > li').allInnerTexts()
      assert.ok(hits.every(item => item.includes('fresh-12')), 'every hit matches')
      report.steps.push({ name: 'rules: search hit', query: 'FRESH-12', matched: 100, first: hits[0], last: hits.at(-1) })
      assert.equal(await searchFor(big, 'zzz-no-match'), '匹配 0 条')
      assert.equal(await big.locator('[data-search-empty]').innerText(), '没有匹配「zzz-no-match」的规则。')
      await shot(desktop, dialog, join(evidence, 'search-miss-desktop-light.png'))
      report.steps.push({ name: 'rules: search miss', query: 'zzz-no-match' })
      await big.getByRole('button', { name: '清空搜索' }).click()
      assert.equal(await entryCount(big, 'added'), 3000)
      await collapseFile(dialog, bigPath)

      // 300 new nodes: pages, and search by name and by server:port.
      const backupPath = 'providers/备用机场.yaml'
      const { detail: backupNodes } = await expandFile(dialog, backupPath, 'desktop')
      assert.match(await flat(backupNodes), /新增 300 个节点 .*D 备用 001 新增 — socks5 · backup\.lab\.invalid:20001.*加载更多（还剩 280 个）/)
      assert.equal(await loadAll(backupNodes, 'added'), 6)
      assert.equal(await entryCount(backupNodes, 'added'), 300)
      // Loaded pages join the same table.
      assert.equal(await group(backupNodes, 'added').locator('[data-node-table]').count(), 1)
      assert.deepEqual((await nodeTable(backupNodes, 'added')).nodes.at(-1), [['D 备用 300', '新增', '—', 'socks5 · backup.lab.invalid:20300']])
      assert.equal(await searchFor(backupNodes, 'backup.lab.invalid:2025'), '匹配 10 个')
      const byServer = await nodeTable(backupNodes, 'added')
      assert.deepEqual(byServer.head, ['节点', '变更项', '变更前', '变更后'])  // matches in the same table
      assert.deepEqual(byServer.nodes.map(node => node[0][3]), Array.from({ length: 10 }, (_, index) => `socks5 · backup.lab.invalid:2025${index}`))
      assert.equal(await searchFor(backupNodes, 'd 备用 29'), '匹配 10 个')
      assert.deepEqual((await nodeTable(backupNodes, 'added')).nodes.map(node => node[0][0]),
        Array.from({ length: 10 }, (_, index) => `D 备用 29${index}`))
      await scrollToTop(dialog, `[data-file="${backupPath}"]`)
      await shot(desktop, dialog, join(evidence, 'search-nodes-desktop-light.png'))
      await noOverflow(desktop, dialog, 'desktop, node search')
      report.steps.push({ name: 'nodes: every page and search', added: 300 })
      await collapseFile(dialog, backupPath)
      await scrollToTop(dialog, '[data-push-diff-changed]')
    }

    await setHidden(desktop, dialog, true, 'desktop')
    const hiddenText = (await dialog.innerText()).replace(/\s+/g, ' ')
    report.steps.push({ name: 'hidden (desktop, light)', text: hiddenText.slice(0, 2400) })
    assert.match(hiddenText, /secret 新增 — 已隐藏/)
    assert.match(hiddenText, /printer\.lab\.invalid 新增 — 198\.51\.100\.30/)  // addresses stay
    const hiddenProxies = (await dialog.locator('[data-key="proxies"] + tr').innerText()).replace(/\s+/g, ' ')
    assert.match(hiddenProxies, /~ 手动 SS password：已隐藏 → 已隐藏/)
    if (pullToken) assert.match(await backup.innerText(), /proxies\?token=已隐藏/)
    await assertNoSecrets(desktop)
    if (LOCAL) {
      const feedDetail = dialog.locator('[data-file="providers/实验机场.yaml"] + tr')
      const hiddenNodes = await nodeTable(feedDetail, 'modified')
      assert.deepEqual(hiddenNodes.nodes[0], [['A 香港', 'server', 'edge-a.lab.invalid', 'edge-a2.lab.invalid'],  // servers stay
        ['port', '11005', '11008'], ['password', '已隐藏', '已隐藏'], ['plugin-opts.host', 'cdn-a.lab.invalid', 'cdn-b.lab.invalid']])
      // The next page arrives while hidden: its credentials never reach the page.
      await loadAll(feedDetail, 'modified')
      assert.equal(await entryCount(feedDetail, 'modified'), 41)
      assert.deepEqual((await nodeTable(feedDetail, 'modified')).nodes.at(-1), [['F 批量 40', 'password', '已隐藏', '已隐藏']])
      assert.equal(await feedDetail.locator('[data-group="modified"] [data-secret-hidden]').count(), 82)
      const page = await desktop.content()
      for (const secret of BULK_SECRETS) assert.ok(!page.includes(secret), `secret on a later page visible: ${secret}`)
      assert.equal(await searchFor(feedDetail, '批量 3'), '匹配 10 个')
      assert.equal(await feedDetail.locator('[data-group="modified"] [data-secret-hidden]').count(), 20)
      assert.deepEqual((await nodeTable(feedDetail, 'modified')).nodes,
        Array.from({ length: 10 }, (_, index) => [[`F 批量 3${index}`, 'password', '已隐藏', '已隐藏']]))
      const searched = await desktop.content()
      for (const secret of BULK_SECRETS) assert.ok(!searched.includes(secret), `secret in a search result visible: ${secret}`)
      report.steps.push({ name: 'hidden: later pages and search results', modified: 41, hiddenPieces: 82 })
      await feedDetail.getByRole('button', { name: '清空搜索' }).click()
      await noOverflow(desktop, dialog, 'desktop, hidden, every node page')
      await scrollToTop(dialog, '[data-file="providers/实验机场.yaml"]')
      await shot(desktop, dialog, join(shots, 'push-diff-file-details-hidden.png'))
      await scrollToTop(dialog, '[data-push-diff-changed]')
    }
    await scrollToTop(dialog, '#push-diff-config-title')  // 表 2: secret hidden, domains and addresses as they are
    await shot(desktop, dialog, join(shots, 'push-diff-hidden.png'))
    await dialog.locator('[data-push-diff-config]').scrollIntoViewIfNeeded()
    await openRaw(dialog)
    const hiddenRaw = await dialog.locator('#push-diff-raw-text').innerText()
    assert.match(hiddenRaw, /\+secret: 已隐藏/)
    assert.match(hiddenRaw, /\+ {2}printer\.lab\.invalid: 198\.51\.100\.30/)
    await assertNoSecrets(desktop)
    await dialog.locator('[data-push-diff-raw]').scrollIntoViewIfNeeded()
    await shot(desktop, dialog, join(evidence, 'raw-hidden-desktop-light.png'))

    await setHidden(desktop, dialog, false, 'desktop')
    if (LOCAL) await assertSecretsShown(desktop, BULK_SECRETS)  // the later page, loaded while hidden
    const raw = await dialog.locator('#push-diff-raw-text').innerText()
    assert.match(raw, /\+secret: lab-controller-secret-5d20/)
    assert.match(raw, /\+ {2}printer\.lab\.invalid: 198\.51\.100\.30/)
    report.steps.push({ name: 'raw diff (values shown again)', lines: raw.split('\n').length })
    await assertSecretsShown(desktop, shown)
    await dialog.locator('[data-push-diff-raw]').scrollIntoViewIfNeeded()
    await shot(desktop, dialog, join(shots, 'push-diff-raw.png'))
    await noOverflow(desktop, dialog, 'desktop')
    // Not remembered: the next opening shows values again.
    await desktop.keyboard.press('Escape')
    const again = await openDialog(desktop, LAB, '有变更，待推送', '[data-push-diff-changed]')
    assert.equal(await again.locator('[data-push-diff-hide]').getByRole('switch').getAttribute('aria-checked'), 'false')
    assert.match((await again.innerText()).replace(/\s+/g, ' '), /secret 新增 — lab-controller-secret-5d20/)
    report.steps.push({ name: 'reopened: values shown, switch off' })
    if (LOCAL) {
      // The configuration changes under the open dialog: the next page is refused, not mixed in.
      const bigPath = 'ruleset/实验大规则集.list'
      const { detail: big } = await expandFile(again, bigPath, 'desktop, before the change')
      await bump()
      await group(big, 'added').locator('[data-load-more]').click()
      const alert = big.locator('[data-details-stale]')
      await alert.waitFor()
      assert.match(await flat(alert), /对比结果已更新，请重新加载。/)
      assert.equal(await entryCount(big, 'added'), 20)
      assert.equal(await big.locator('[data-load-more]').count(), 0)
      const focused = await desktop.evaluate(() => document.activeElement?.textContent?.trim())
      assert.equal(focused, '重新加载')
      await scrollToTop(again, `[data-file="${bigPath}"]`)
      await shot(desktop, again, join(shots, 'push-diff-stale.png'))
      await alert.getByRole('button', { name: '重新加载' }).click()
      await again.locator('[data-push-diff-loading]').waitFor()
      await again.locator('[data-push-diff-changed]').waitFor({ timeout: 180000 })
      const reopened = again.locator(`[data-file="${bigPath}"] + tr [data-file-details]`)
      await reopened.waitFor()
      assert.equal(await reopened.locator('[data-details-stale]').count(), 0)
      await loadMore(reopened, 'added')
      assert.ok((await flat(group(reopened, 'added'))).includes('加载更多（还剩 2930 条）'))
      assert.match(await flat(again), /实验直连.*规则 \+3 \/ −1/)  // the new comparison includes the change
      report.steps.push({ name: 'stale: reload keeps the file open and pages again', focused })
      await revertBump()

      // A group of more than 20 rows starts folded; its title opens and closes it, by mouse or keyboard.
      await addHosts()
      await desktop.keyboard.press('Escape')
      const folded = await openDialog(desktop, LAB, '有变更，待推送', '[data-push-diff-changed]')
      const hostsGroup = folded.locator('[data-config-group="hosts"]')
      const hostsToggle = hostsGroup.locator('[data-group-toggle]')
      const groupsFolded = await configOutline(folded)
      report.steps.push({ name: 'table 2: a group of 25 rows', groups: groupsFolded.map(group => [group.title, group.expanded, group.lines.length]) })
      assert.deepEqual(groupsFolded.find(group => group.title.startsWith('Hosts')), { title: 'Hosts hosts 新增 25 项', expanded: 'false', lines: [] })
      assert.ok(groupsFolded.filter(group => !group.title.startsWith('Hosts')).every(group => group.expanded === 'true' && group.lines.length))
      await scrollToTop(folded, '#push-diff-config-title')
      await shot(desktop, folded, join(shots, 'push-diff-config-folded.png'))
      await hostsToggle.focus()
      await desktop.keyboard.press('Enter')
      assert.equal(await hostsToggle.getAttribute('aria-expanded'), 'true')
      assert.equal(await hostsGroup.locator(':scope > tr[data-key]').count(), 25)
      await desktop.keyboard.press('Space')
      assert.equal(await hostsToggle.getAttribute('aria-expanded'), 'false')
      assert.equal(await hostsGroup.locator(':scope > tr[data-key]').count(), 0)
      await hostsToggle.click()
      assert.equal(await hostsGroup.locator(':scope > tr[data-key]').count(), 25)
      await noOverflow(desktop, folded, 'desktop, 25 hosts open')
      report.steps.push({ name: 'table 2: folded group opened by Enter, closed by Space, opened by click' })
      await revertHosts()
    }

    const phone = await page('dark', { width: 375, height: 812 })
    const mobile = await openDialog(phone, LAB, '有变更，待推送', '[data-push-diff-changed]')
    const touch = report.steps.at(-1)
    assert.ok(touch.height >= 44, `phone sync label ${touch.height}px high`)
    await noOverflow(phone, mobile, 'phone')
    await shot(phone, mobile, join(shots, 'push-diff-mobile-dark.png'))
    if (LOCAL) {
      const { detail: feed, box } = await expandFile(mobile, 'providers/实验机场.yaml', 'phone')
      assert.ok(box && box.height >= 44, `phone 展开明细 ${box?.height}px high`)
      // Records: the node's name as the title, then one line per change; no column header in sight.
      const records = await nodeRecords(feed, 'modified', 1)
      report.steps.push({ name: 'phone: node records', ...records })
      assert.ok(records.display === 'block' && records.head <= 1, `phone node table ${JSON.stringify(records)}`)
      assert.deepEqual(records.records[0], ['A 香港', 'server：edge-a.lab.invalid → edge-a2.lab.invalid', 'port：11005 → 11008',
        `password：${NODE_SECRETS[0]} → ${NODE_SECRETS[1]}`, 'plugin-opts.host：cdn-a.lab.invalid → cdn-b.lab.invalid'])
      const addedRecords = (await nodeRecords(feed, 'added')).records
      assert.equal(addedRecords[0][0], 'C 新增节点')
      assert.match(addedRecords[0][1], /^新增 ?trojan · edge-c\.lab\.invalid:443$/)  // no empty 变更前
      assert.match((await nodeRecords(feed, 'removed')).records[0].join('\n'), /^E 下线\n删除 ?socks5 · \[2001:db8::e\]:1080$/)
      const nodeMore = (await group(feed, 'modified').locator('[data-load-more]').boundingBox())?.height
      assert.ok(nodeMore >= 44, `phone node 加载更多 ${nodeMore}px high`)
      await noOverflow(phone, mobile, 'phone, node records')
      const { detail: big } = await expandFile(mobile, 'ruleset/实验大规则集.list', 'phone')
      // Touch targets of 44px: the search box, its clear button and 加载更多.
      const more = group(big, 'added').locator('[data-load-more]')
      const sizes = { more: (await more.boundingBox())?.height, search: (await big.locator('[data-detail-search]').boundingBox())?.height }
      await loadMore(big, 'added')
      await scrollToTop(mobile, '[data-file="ruleset/实验大规则集.list"]')
      await innerScroll(phone, mobile, big, 'phone', Math.round(812 * 0.6))
      assert.equal(await searchFor(big, 'fresh-12'), '匹配 100 条')
      sizes.clear = (await big.getByRole('button', { name: '清空搜索' }).boundingBox())?.height
      report.steps.push({ name: 'phone: touch targets', ...sizes })
      for (const [key, value] of Object.entries(sizes)) assert.ok(value >= 44, `phone ${key} ${value}px high`)
      await noOverflow(phone, mobile, 'phone, search')
      await scrollToTop(mobile, '[data-file="ruleset/实验大规则集.list"]')
      await big.locator('[data-details-scroll]').evaluate(element => { element.scrollTop = 0 })
      await shot(phone, mobile, join(shots, 'push-diff-search-mobile-dark.png'))
      await big.getByRole('button', { name: '清空搜索' }).click()
      await expandFile(mobile, 'providers/备用机场.yaml', 'phone')
      await noOverflow(phone, mobile, 'phone, file details open')
      await assertSecretsShown(phone, NODE_SECRETS)
      await setHidden(phone, mobile, true, 'phone, file details')
      assert.ok((await flat(feed)).includes('password：已隐藏 → 已隐藏'))
      await assertNoSecrets(phone)
      await noOverflow(phone, mobile, 'phone, file details hidden')
      await scrollToTop(mobile, '[data-file="providers/实验机场.yaml"]')
      await shot(phone, mobile, join(shots, 'push-diff-file-details-mobile-dark.png'))
      await setHidden(phone, mobile, false, 'phone, file details')
      await assertSecretsShown(phone, NODE_SECRETS)
      await scrollToTop(mobile, '[data-file="providers/实验机场.yaml"]')
      await shot(phone, mobile, join(evidence, 'details-shown-mobile-dark.png'))
      await scrollToTop(mobile, '[data-file="ruleset/实验大规则集.list"]')
      await shot(phone, mobile, join(evidence, 'details-pages-mobile-dark.png'))
      // Node search on the phone: matches as the same records.
      assert.equal(await searchFor(feed, '批量 3'), '匹配 10 个')
      const found = await nodeRecords(feed, 'modified', 10)
      assert.deepEqual(found.records, Array.from({ length: 10 }, (_, index) => [`F 批量 3${index}`,
        `password：lab-bulk-pass-before-3${index} → lab-bulk-pass-after-3${index}`]))
      await noOverflow(phone, mobile, 'phone, node search')
      await scrollToTop(mobile, '[data-file="providers/实验机场.yaml"]')
      await shot(phone, mobile, join(evidence, 'search-nodes-mobile-dark.png'))
      await feed.getByRole('button', { name: '清空搜索' }).click()
    }
    if (LOCAL) {
      // 表 2 on the phone: group titles as separators (44px buttons), records of 配置项：原值 → 新值, chip on the right.
      const toggleHeights = await mobile.locator('[data-group-toggle]').evaluateAll(buttons => buttons.map(button => Math.round(button.getBoundingClientRect().height)))
      const dnsRecords = await configRecords(mobile, 'dns')
      const hostRecords = await configRecords(mobile, 'hosts')
      const basicRecords = await configRecords(mobile, 'basic')
      const breaks = await breaksOnlyAtSeparators(mobile)
      report.steps.push({ name: 'phone: table 2 records', toggleHeights, dnsRecords, hostRecords, basicRecords, breaks })
      assert.ok(toggleHeights.length === 6 && toggleHeights.every(height => height >= 44), `phone group titles ${toggleHeights}`)
      assert.match(dnsRecords[0].line, /^fake-ip-filter：2 条 → 3 条 \+1 \/ −0 ?展开明细$/)
      assert.deepEqual(dnsRecords.slice(1).map(record => record.line), POLICY_DOMAINS.map(domain => `${domain}：198.51.100.54`))
      assert.deepEqual(dnsRecords.map(record => record.nested), [false, true, true, true])
      assert.deepEqual(hostRecords.map(record => record.line), HOSTS.map(([name, address]) => `${name}：${address}`))
      assert.deepEqual(basicRecords.map(record => record.line), ['log-level：info → debug', 'unified-delay：true', `secret：${SECRETS[2]}`])
      assert.ok([...dnsRecords, ...hostRecords, ...basicRecords].every(record => record.chipRight <= 16 && record.chipTop <= 12), 'chips at the top right')
      assert.deepEqual(breaks.broken, [], `phone: names broken inside a word: ${breaks.broken}`)
      assert.ok(breaks.wrapped.length > 0, 'some long name wraps on the phone (at a separator)')
      await noOverflow(phone, mobile, 'phone, table 2')
      await scrollToTop(mobile, '#push-diff-config-title')
      await shot(phone, mobile, join(shots, 'push-diff-config-groups-mobile-dark.png'))
    }
    await expand(mobile, 'proxies')
    await noOverflow(phone, mobile, 'phone, details open')
    await assertSecretsShown(phone, shown)
    await setHidden(phone, mobile, true, 'phone')
    await openRaw(mobile)
    await assertNoSecrets(phone)
    await noOverflow(phone, mobile, 'phone, hidden, raw diff open')
    await mobile.locator('[data-key="proxies"]').scrollIntoViewIfNeeded()
    await shot(phone, mobile, join(evidence, 'hidden-mobile-dark.png'))
    await setHidden(phone, mobile, false, 'phone')
    await assertSecretsShown(phone, shown)
    await mobile.locator('[data-key="proxies"]').scrollIntoViewIfNeeded()
    await shot(phone, mobile, join(evidence, 'config-mobile-dark.png'))
  } else if (scene === 'nobaseline') {
    const desktop = await page('light', { width: 1280, height: 900 })
    const never = await openDialog(desktop, '实验 · 从未推送', '尚未推送', '[data-push-diff-unavailable]')
    const neverText = (await never.innerText()).replace(/\s+/g, ' ')
    assert.match(neverText, /没有可对比的基准.*还没有成功推送过配置/)
    report.steps.push({ name: 'never pushed', text: neverText })
    await shot(desktop, never, join(shots, 'push-diff-no-baseline.png'))
    await desktop.keyboard.press('Escape')
    const legacy = await openDialog(desktop, '实验 · 旧版本推送', '同步状态待确认', '[data-push-diff-unavailable]')
    const legacyText = (await legacy.innerText()).replace(/\s+/g, ' ')
    assert.match(legacyText, /没有可对比的基准.*旧版本/)
    report.steps.push({ name: 'old version push', text: legacyText })
    await shot(desktop, legacy, join(evidence, 'legacy-desktop-light.png'))
  } else if (scene === 'error') {
    const desktop = await page('light', { width: 1280, height: 900 })
    const { button } = await syncButton(desktop, '实验 · 旧版本推送', '同步状态待确认')
    // The record disappears after the page loaded (another tab deleted it): the request fails for real.
    const removed = await fetch(`http://127.0.0.1:${apiPort}/api/agents/lab-old-version`, {
      method: 'DELETE', headers: { Authorization: `Bearer ${token}` } })
    assert.equal(removed.status, 200)
    await button.click()
    const dialog = desktop.locator('[data-push-diff]')
    await dialog.locator('[data-push-diff-error]').waitFor({ timeout: 30000 })
    const text = (await dialog.innerText()).replace(/\s+/g, ' ')
    assert.match(text, /无法读取待推送的变更.*找不到这台 Agent/)
    report.steps.push({ name: 'request error', text })
    await desktop.mouse.move(0, 0)
    await shot(desktop, dialog, join(shots, 'push-diff-error.png'))
    await dialog.getByRole('button', { name: '重试' }).click()
    await dialog.locator('[data-push-diff-error]').waitFor({ timeout: 30000 })
    report.steps.push({ name: 'retry fails again with the same reason' })
  } else if (scene === 'synced') {
    const phone = await page('dark', { width: 375, height: 812 })
    const dialog = await openDialog(phone, LAB, '已同步', '[data-push-diff-synced]')
    const text = (await dialog.innerText()).replace(/\s+/g, ' ')
    assert.match(text, /无变更/)
    assert.doesNotMatch(text, /推送 *$/)
    report.steps.push({ name: 'synced after push (phone, dark)', text })
    await shot(phone, dialog, join(shots, 'push-diff-synced-mobile-dark.png'))
  } else {
    throw new Error('scene: changed | nobaseline | error | synced')
  }
  assert.deepEqual(report.browserErrors, [])
  console.log(JSON.stringify(report, null, 1))
} finally {
  await revertBump().catch(error => report.steps.push({ name: 'revert failed', error: String(error) }))
  await revertHosts().catch(error => report.steps.push({ name: 'revert hosts failed', error: String(error) }))
  await writeFile(join(evidence, `browser-${scene}.json`), JSON.stringify(report, null, 2))
  await browser?.close()
  await vite?.close()
}
