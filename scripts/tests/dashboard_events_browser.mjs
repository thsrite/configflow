// Run via rt on the test server after cf-test prepares frontend dependencies.
// Actual Dashboard and styles, fixture APIs only; wheel/keyboard/touch must reach the last event.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { lstat, mkdtemp, mkdir, readFile, writeFile, symlink, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'

const root = process.cwd()
const frontend = resolve(root, 'frontend')
// rt may copy the editor's Mac symlink; reuse cf-test's prepared server cache.
const hash = createHash('sha256').update(await readFile(join(frontend, 'package-lock.json'))).digest('hex').slice(0, 16)
const modules = `/home/sunbroke/cf-test/node_modules/${hash}/node_modules`
await lstat(modules)
await rm(join(frontend, 'node_modules'), { force: true })
await symlink(modules, join(frontend, 'node_modules'))
const require = createRequire(join(frontend, 'package.json'))
const { createServer } = await import(join(frontend, 'node_modules/vite/dist/node/index.js'))
const vue = (await import(join(frontend, 'node_modules/@vitejs/plugin-vue/dist/index.mjs'))).default
const tailwind = (await import(require.resolve('@tailwindcss/vite'))).default
const { chromium } = await import(process.env.CF_PLAYWRIGHT ||
  '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs')
const directory = await mkdtemp(join(tmpdir(), 'configflow-dashboard-events-'))
const output = resolve(root, '.ci-results/dashboard-events')
await mkdir(output, { recursive: true })
await symlink(join(frontend, 'node_modules'), join(directory, 'node_modules'))
await writeFile(join(directory, 'index.html'), '<!doctype html><html lang="zh-CN" data-motion="off"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>数据统计事件流</title><div id="app"></div><script type="module" src="/entry.js"></script></html>')
await writeFile(join(directory, 'style.css'), `@import '${frontend}/src/styles/theme.css';\n@import '${frontend}/src/styles/tokens.css';\n@source '${frontend}/src';\n`)
await writeFile(join(directory, 'entry.js'), `
import { createApp, h } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import Dashboard from '@/views/Dashboard.vue'
import './style.css'
const params = new URLSearchParams(location.search)
document.documentElement.dataset.theme = params.get('theme')
document.documentElement.dataset.accent = params.get('accent')
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Dashboard }] })
await router.push('/')
createApp({ render: () => h('main', { class: 'mx-auto max-w-[1360px] p-3 sm:p-6' }, [h(Dashboard)]) }).use(router).mount('#app')
`)

const messages = ['服务已启动', '规则缓存已读取', '订阅缓存已读取', '节点库已读取', '配置模板已读取',
  '配置检查完成', '设备状态已更新', '规则集检查完成', '订阅更新完成', '生成配置完成', '设备心跳已收到',
  '配置发布完成', '这条很长的日志用于确认窄屏只截断文字，不会挤出页面或挡住后续事件']
const logs = messages.map((message, i) => `2026-11-01 15:00:${String(i).padStart(2, '0')},000 - backend.events - INFO - ${message}`)
let server, browser
const report = { fixture: true, errors: [], cases: [] }

async function lastEventVisible(stream) {
  // Wait for TransitionGroup's enter/move animation when a refresh replaces the rows.
  await stream.page().waitForFunction(element => {
    const rows = element.querySelectorAll('time')
    const bounds = element.getBoundingClientRect()
    const row = rows[rows.length - 1]?.parentElement.getBoundingClientRect()
    return !!row && row.top >= bounds.top - 1 && row.bottom <= bounds.bottom + 1 && getComputedStyle(element).maskImage === 'none'
  }, await stream.elementHandle(), { timeout: 3000 })
  return true
}

try {
  server = await createServer({ configFile: false, root: directory, plugins: [vue(), tailwind()],
    resolve: { alias: { '@': join(frontend, 'src') } },
    server: { host: '127.0.0.1', port: 0, fs: { allow: [directory, frontend] } }, logLevel: 'warn' })
  await server.listen()
  browser = await chromium.launch({ headless: true })
  const url = `http://127.0.0.1:${server.httpServer.address().port}`
  for (const width of [1360, 375]) for (const theme of ['light', 'dark']) for (const accent of ['clay', 'kraft', 'sky']) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: 'reduce',
      isMobile: width < 500, hasTouch: width < 500, locale: 'zh-CN' })
    page.on('pageerror', error => report.errors.push(error.message))
    let currentLogs = logs
    await page.route(url => url.pathname.startsWith('/api/'), route => {
      assert.equal(route.request().method(), 'GET', 'Dashboard checks must not perform writes')
      const path = new URL(route.request().url()).pathname
      const fixtures = {
        '/api/logs/tail': { logs: currentLogs }, '/api/stats/overview': { success: true, data: {} },
        '/api/subscriptions': [], '/api/subscriptions/health': { items: [] }, '/api/proxy-groups': [],
        '/api/rule-sets': [], '/api/nodes': [], '/api/nodes/latency': { results: {} }, '/api/agents': [],
        '/api/setup': { sessions: [], suggested: false },
      }
      assert.ok(Object.hasOwn(fixtures, path), `Unexpected API request: ${path}`)
      return route.fulfill({ json: fixtures[path] })
    })
    await page.goto(`${url}/?theme=${theme}&accent=${accent}`)
    const card = page.getByRole('region', { name: '事件流', exact: true })
    const stream = card.locator('.event-stream')
    await card.getByText('12 条', { exact: true }).waitFor()
    await page.evaluate(() => document.fonts.ready)
    await stream.scrollIntoViewIfNeeded()
    assert.equal(await stream.locator('time').count(), 12)
    assert.equal(await stream.locator('time').last().innerText(), '15:00:01', 'Keep the most recent 12 events')
    const before = await stream.evaluate(el => ({ height: el.clientHeight, contentHeight: el.scrollHeight,
      fullyVisible: [...el.querySelectorAll('time')].filter(time => time.parentElement.getBoundingClientRect().bottom <= el.getBoundingClientRect().bottom).length }))
    assert.ok(before.contentHeight > before.height, 'Fixture must exercise overflow')
    if (width >= 500) {
      await stream.hover()
      await page.mouse.wheel(0, 1000)
    } else {
      const box = await stream.boundingBox()
      const cdp = await page.context().newCDPSession(page)
      const x = box.x + box.width / 2, start = box.y + box.height - 20
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y: start }] })
      for (let distance = 30; distance <= 300; distance += 30) {
        await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y: start - distance }] })
      }
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
      await cdp.detach()
    }
    await page.waitForFunction(() => {
      const el = document.querySelector('.event-stream')
      return el.scrollTop + el.clientHeight >= el.scrollHeight - 1
    }, null, { timeout: 3000 })
    assert.ok(await lastEventVisible(stream), 'The last row must be fully readable without a fade mask')
    if (width >= 500) {
      await stream.focus()
      await page.keyboard.press('Home')
      await page.waitForFunction(() => document.querySelector('.event-stream').scrollTop === 0)
      await page.keyboard.press('End')
      await page.waitForFunction(() => {
        const el = document.querySelector('.event-stream')
        return el.scrollTop + el.clientHeight >= el.scrollHeight - 1
      })
      assert.ok(await lastEventVisible(stream), 'Keyboard users must reach the last row too')
      const health = await page.getByRole('region', { name: '订阅健康', exact: true }).boundingBox()
      const eventBox = await card.boundingBox()
      assert.ok(Math.abs(eventBox.y + eventBox.height - health.y - health.height) <= 1, 'Keep desktop card bottoms aligned')
    }
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    await stream.evaluate(el => el.blur())
    await card.screenshot({ path: join(output, `${width}-${theme}-${accent}.png`) })
    if (accent === 'clay' && ((width === 1360 && theme === 'light') || (width === 375 && theme === 'dark'))) {
      await card.screenshot({ path: resolve(root, `doc/img/guide/dashboard-events-${width === 1360 ? 'desktop' : 'mobile'}-${theme}.png`) })
    }
    currentLogs = logs.slice(0, 1)
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await card.getByText('1 条', { exact: true }).waitFor()
    await page.waitForFunction(() => document.querySelectorAll('.event-stream time').length === 1)
    assert.equal(await stream.locator('time').count(), 1)
    assert.ok(await lastEventVisible(stream))
    currentLogs = []
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await card.getByText('还没有日志事件', { exact: true }).waitFor()
    await page.waitForFunction(() => document.querySelectorAll('.event-stream time').length === 0)
    assert.equal(await stream.locator('time').count(), 0)
    assert.notEqual(await stream.getAttribute('tabindex'), '0', 'Do not add an empty tab stop')
    report.cases.push({ width, theme, accent, ...before })
    await page.close()
  }
  assert.deepEqual(report.errors, [])
  report.passed = true
  console.log(JSON.stringify(report))
} finally {
  await writeFile(join(output, 'report.json'), JSON.stringify(report, null, 2))
  await browser?.close()
  await server?.close()
  await rm(directory, { recursive: true, force: true })
}
