#!/usr/bin/env node
// 「待推送的变更」 in the real app, against a private ConfigFlow (no API mocks): the PVE lab one of
// push_diff_lab.py, or the device-less one of push_diff_local.py (CF_PUSHDIFF_WORK=/tmp/cf-pushdiff-local
// CF_PUSHDIFF_AGENT='实验 Mihomo'). Test server only, after `cf-test typecheck` linked frontend/node_modules:
//   rt --no-sync --back doc/img/guide --back .ci-results/push-diff -- node scripts/tests/push_diff_browser.mjs changed
//   ... push_diff_browser.mjs nobaseline | error | synced   (lab data only)
// Reads only: never clicks 推送 (the lab script publishes). `error` deletes the old-version record through the API.
// `changed` checks values are shown by default, that 隐藏敏感信息 leaves no credential in the page (tables,
// details, raw diff) and that switching it off shows them again, on a light desktop and a dark phone; with the
// local data it also expands the subscription and rule set rows (nodes added, removed, modified field by field,
// a repeated name, rules added and removed), loads every page of the large ones (+3000 / −1500 rules, +300 nodes)
// inside their own scroll area, searches them (hits and misses), checks that credentials on pages loaded while
// 隐藏敏感信息 is on never reach the page, and changes the configuration under an open dialog to see 对比结果已更新.
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
  report.steps.push({ name: `${name}: no horizontal overflow`, page, body, tables })
  assert.ok(page <= 0 && body <= 0 && tables.every(value => value <= 0), `horizontal overflow ${JSON.stringify({ page, body, tables })}`)
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
const entryCount = async (detail, change) => group(detail, change).locator(':scope > ul > li').count()

/** 加载更多 once and wait for the page to be added. */
async function loadMore(detail, change) {
  const section = group(detail, change)
  const before = await entryCount(detail, change)
  await section.locator('[data-load-more]').click()
  await detail.page().waitForFunction(([element, count]) => element.querySelectorAll(':scope > ul > li').length > count
    && !element.querySelector('[data-load-more][disabled]'), [await section.elementHandle(), before])
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
    assert.match(text, /hosts\.printer\.lab\.invalid 新增 — 198\.51\.100\.30/)
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
    await shot(desktop, dialog, join(shots, 'push-diff-changed.png'))

    if (LOCAL) {
      // Table 1 rows expand into nodes and rules, grouped by change, values as they are.
      const { detail: feed } = await expandFile(dialog, 'providers/实验机场.yaml', 'desktop')
      const nodes = await flat(feed)
      report.steps.push({ name: 'subscription details (desktop, light)', text: nodes })
      for (const part of ['新增 2 个节点', '+ C 新增节点 · trojan · edge-c.lab.invalid:443',
        '+ B 日本（同名第 2 个） · socks5 · [2001:db8::b]:1080', '删除 1 个节点', '− E 下线', '修改 41 个节点', '~ A 香港',
        'server：edge-a.lab.invalid → edge-a2.lab.invalid', 'port：11005 → 11008',
        'password：lab-node-pass-before-41f2 → lab-node-pass-after-8d3c', 'plugin-opts.host：cdn-a.lab.invalid → cdn-b.lab.invalid',
        '~ F 批量 19', 'password：lab-bulk-pass-before-19 → lab-bulk-pass-after-19', '加载更多（还剩 21 个）',
        '有 1 个节点名被多个节点共用']) assert.ok(nodes.includes(part), `subscription details miss: ${part}`)
      assert.ok(!nodes.includes('F 批量 20'), 'the first page ends at 20 modified nodes')
      assert.ok(!nodes.includes('lab-trojan-pass-c7'), 'a new node shows type and address only')
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
      assert.match(await flat(backupNodes), /新增 300 个节点 .*D 备用 001 · socks5 · backup\.lab\.invalid:20001.*加载更多（还剩 280 个）/)
      assert.equal(await loadAll(backupNodes, 'added'), 6)
      assert.equal(await entryCount(backupNodes, 'added'), 300)
      assert.equal(await searchFor(backupNodes, 'backup.lab.invalid:2025'), '匹配 10 个')
      assert.equal(await searchFor(backupNodes, 'd 备用 29'), '匹配 10 个')
      assert.deepEqual((await group(backupNodes, 'added').locator(':scope > ul > li').allInnerTexts()).map(item => item.split(' · ')[0]),
        Array.from({ length: 10 }, (_, index) => `+ D 备用 29${index}`))
      report.steps.push({ name: 'nodes: every page and search', added: 300 })
      await collapseFile(dialog, backupPath)
      await scrollToTop(dialog, '[data-push-diff-changed]')
    }

    await setHidden(desktop, dialog, true, 'desktop')
    const hiddenText = (await dialog.innerText()).replace(/\s+/g, ' ')
    report.steps.push({ name: 'hidden (desktop, light)', text: hiddenText.slice(0, 2400) })
    assert.match(hiddenText, /secret 新增 — 已隐藏/)
    assert.match(hiddenText, /hosts\.printer\.lab\.invalid 新增 — 198\.51\.100\.30/)  // addresses stay
    const hiddenProxies = (await dialog.locator('[data-key="proxies"] + tr').innerText()).replace(/\s+/g, ' ')
    assert.match(hiddenProxies, /~ 手动 SS password：已隐藏 → 已隐藏/)
    if (pullToken) assert.match(await backup.innerText(), /proxies\?token=已隐藏/)
    await assertNoSecrets(desktop)
    if (LOCAL) {
      const feedDetail = dialog.locator('[data-file="providers/实验机场.yaml"] + tr')
      const feed = await flat(feedDetail)
      assert.ok(feed.includes('password：已隐藏 → 已隐藏'), 'node password hidden in the details')
      assert.ok(feed.includes('server：edge-a.lab.invalid → edge-a2.lab.invalid'), 'servers stay')
      // The next page arrives while hidden: its credentials never reach the page.
      await loadAll(feedDetail, 'modified')
      assert.equal(await entryCount(feedDetail, 'modified'), 41)
      assert.ok((await flat(feedDetail)).includes('~ F 批量 40 password：已隐藏 → 已隐藏'))
      assert.equal(await feedDetail.locator('[data-group="modified"] [data-secret-hidden]').count(), 82)
      const page = await desktop.content()
      for (const secret of BULK_SECRETS) assert.ok(!page.includes(secret), `secret on a later page visible: ${secret}`)
      assert.equal(await searchFor(feedDetail, '批量 3'), '匹配 10 个')
      assert.equal(await feedDetail.locator('[data-group="modified"] [data-secret-hidden]').count(), 20)
      const searched = await desktop.content()
      for (const secret of BULK_SECRETS) assert.ok(!searched.includes(secret), `secret in a search result visible: ${secret}`)
      report.steps.push({ name: 'hidden: later pages and search results', modified: 41, hiddenPieces: 82 })
      await feedDetail.getByRole('button', { name: '清空搜索' }).click()
      await scrollToTop(dialog, '[data-file="providers/实验机场.yaml"]')
      await shot(desktop, dialog, join(evidence, 'details-hidden-desktop-light.png'))
      await scrollToTop(dialog, '[data-push-diff-changed]')
    }
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
  await writeFile(join(evidence, `browser-${scene}.json`), JSON.stringify(report, null, 2))
  await browser?.close()
  await vite?.close()
}
