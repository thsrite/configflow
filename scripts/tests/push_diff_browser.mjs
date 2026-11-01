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
// a repeated name, rules added and removed) and the ones with more changes than a file lists.
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
    assert.match(text, LOCAL ? /实验机场.*订阅.*修改.*节点 \+2 \/ −1 · 修改 1 · 共 4 个/ : /实验机场.*订阅.*修改.*节点 \+1 \/ −0/)
    assert.match(text, /实验直连.*规则集.*修改.*规则 \+2 \/ −1/)
    if (LOCAL) {
      assert.match(text, /备用机场.*订阅.*新增.*节点 \+130 \/ −0/)
      assert.match(text, /实验大规则集.*规则集.*修改.*规则 \+300 \/ −150/)
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
        '+ B 日本（同名第 2 个） · socks5 · [2001:db8::b]:1080', '删除 1 个节点', '− E 下线', '修改 1 个节点', '~ A 香港',
        'server：edge-a.lab.invalid → edge-a2.lab.invalid', 'port：11005 → 11008',
        'password：lab-node-pass-before-41f2 → lab-node-pass-after-8d3c', 'plugin-opts.host：cdn-a.lab.invalid → cdn-b.lab.invalid',
        '有 1 个节点名被多个节点共用']) assert.ok(nodes.includes(part), `subscription details miss: ${part}`)
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

      // More changes than one file lists: 100 nodes, 200 rules shared between added and removed.
      const { detail: big } = await expandFile(dialog, 'ruleset/实验大规则集.list', 'desktop')
      const bigText = await flat(big)
      assert.match(bigText, /新增 300 条规则.*还有 200 条未列出.*删除 150 条规则.*还有 50 条未列出/)
      assert.equal(await big.locator('[data-file-details] li').count(), 200)
      const { detail: backupNodes } = await expandFile(dialog, 'providers/备用机场.yaml', 'desktop')
      assert.match(await flat(backupNodes), /新增 130 个节点 .*D 备用 001 · socks5 · backup\.lab\.invalid:20001.*还有 30 个未列出/)
      assert.equal(await backupNodes.locator('[data-file-details] li').count(), 100)
      report.steps.push({ name: 'truncated details', rules: bigText.slice(0, 200), nodes: 100 })
      await scrollToTop(dialog, '[data-file="ruleset/实验大规则集.list"] + tr li:nth-child(96)')
      await shot(desktop, dialog, join(evidence, 'details-truncated-desktop-light.png'))
      await noOverflow(desktop, dialog, 'desktop, details open')
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
      const feed = await flat(dialog.locator('[data-file="providers/实验机场.yaml"] + tr'))
      assert.ok(feed.includes('password：已隐藏 → 已隐藏'), 'node password hidden in the details')
      assert.ok(feed.includes('server：edge-a.lab.invalid → edge-a2.lab.invalid'), 'servers stay')
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

    const phone = await page('dark', { width: 375, height: 812 })
    const mobile = await openDialog(phone, LAB, '有变更，待推送', '[data-push-diff-changed]')
    const touch = report.steps.at(-1)
    assert.ok(touch.height >= 44, `phone sync label ${touch.height}px high`)
    await noOverflow(phone, mobile, 'phone')
    await shot(phone, mobile, join(shots, 'push-diff-mobile-dark.png'))
    if (LOCAL) {
      const { detail: feed, box } = await expandFile(mobile, 'providers/实验机场.yaml', 'phone')
      assert.ok(box && box.height >= 44, `phone 展开明细 ${box?.height}px high`)
      await expandFile(mobile, 'ruleset/实验大规则集.list', 'phone')
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
      await scrollToTop(mobile, '[data-file="ruleset/实验大规则集.list"] + tr li:nth-child(97)')
      await shot(phone, mobile, join(evidence, 'details-truncated-mobile-dark.png'))
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
  await writeFile(join(evidence, `browser-${scene}.json`), JSON.stringify(report, null, 2))
  await browser?.close()
  await vite?.close()
}
