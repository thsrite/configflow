// Run through rt on the test server. Real Vue components with documented fixture
// data; no production API, DNS queries, or Agent changes are made by this check.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdtemp, mkdir, writeFile, symlink, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'

const root = process.cwd()
const frontend = resolve(root, 'frontend')
const require = createRequire(join(frontend, 'package.json'))
const { createServer } = await import(join(frontend, 'node_modules/vite/dist/node/index.js'))
const vue = (await import(join(frontend, 'node_modules/@vitejs/plugin-vue/dist/index.mjs'))).default
const tailwind = (await import(require.resolve('@tailwindcss/vite'))).default
const { chromium } = await import(process.env.CF_PLAYWRIGHT ||
  '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs')
const directory = await mkdtemp(join(tmpdir(), 'configflow-publication-stats-'))
const output = resolve(root, '.ci-results/oxidns-publication-stats')
await mkdir(output, { recursive: true })
await symlink(join(frontend, 'node_modules'), join(directory, 'node_modules'))
await writeFile(join(directory, 'index.html'), '<!doctype html><html lang="zh-CN" data-motion="off"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OxiDNS 发布后统计接续示例</title><div id="app"></div><script type="module" src="/entry.js"></script></html>')
await writeFile(join(directory, 'style.css'), `@import '${frontend}/src/styles/theme.css';\n@import '${frontend}/src/styles/tokens.css';\n@source '${frontend}/src';\n`)
const section = rows => ({ sample_size: 9600, at_limit: false, rows: rows.map(([key, count]) => ({ key, count, share: count / 9600 })) })
const data = {
  success: true, agent: { id: 'example', name: 'OxiDNS', host: '192.0.2.2', version: '1.8.53-go', status: 'online' },
  collected_at: '2026-11-01T12:10:00Z', core: { status: 'ok', version: '1.6.1', uptime_ms: 1366800000, instance_id: 'example' },
  recorders: ['query_recorder'],
  statistics: { generation: 'example', started_at_ms: 1793520000000, reason: 'publication', partial: false, publication_count: 2 },
  metrics: { requests_total: 198640, failed_total: 12, avg_latency_ms: 8.34, cache_hit_rate: 75.9259, inflight: 2 },
  caches: [{ tag: 'lazy_cache', lookups: 108000, hits: 82000, fresh_hits: 80500, stale_hits: 1500, misses: 26000, expired: 0, entries: 736, hit_rate: 75.9259 }],
  servers: [{ tag: 'server_udp', protocol: 'udp', requests: 198640, failed: 12, inflight: 2, avg_latency_ms: 8.34 }],
  upstreams: [{ id: 'local', tag: 'forward_local', upstream: 'local', address: '223.5.5.5', requests: 22100, failed: 4, avg_latency_ms: 12.4 }],
  rankings: { success: true, recorder: 'query_recorder', collected_at_ms: 1793535000000, source_revision: 'example',
    instance_id: 'example', retention_days: 7, sample_limit: 10000,
    clients: section([['192.0.2.10', 3900], ['2001:db8:0:0:abcd:ef01:2345:6789', 2600], ['192.0.2.12', 1400]]),
    domains: section([['example.com.', 2300], ['updates.example.net.', 1200], ['api.example.org.', 1000]]),
    blocked: { ...section([['ads.example.test.', 300]]), available: true, reason: '拦截规则' } }
}
await writeFile(join(directory, 'entry.js'), `
import { createApp, h } from 'vue'
import './style.css'
import Overview from '@/components/agents/OxiDNSOverview.vue'
import Controls from '@/components/agents/OxiDNSStatisticsControls.vue'
const data = ${JSON.stringify(data)}
const notes = { '192.0.2.10': '客厅 NAS', '2001:db8::abcd:ef01:2345:6789': '书房工作电脑（有线网卡 · IPv6）' }
const params = new URLSearchParams(location.search)
document.documentElement.dataset.theme = params.get('theme') || 'light'
document.documentElement.dataset.accent = params.get('accent') || 'clay'
createApp({ render: () => h('main', { class: 'mx-auto max-w-[1320px] space-y-5 p-4 sm:p-7' }, [
  h('header', { class: 'space-y-1' }, [h('h1', { class: 'm-0 font-display text-3xl' }, 'OxiDNS 面板'),
    h('p', { class: 'm-0 text-sm text-muted-foreground' }, 'OxiDNS · 192.0.2.2 · 示例数据')]),
  h('div', { class: 'flex flex-wrap items-center justify-end gap-3' }, [h(Controls, { statistics: data.statistics })]),
  h(Overview, { overview: data, qps: 32.8, recorder: 'query_recorder', resetting: false, notes }),
]) }).mount('#app')
`)

let server, browser
const report = { fixture: true, errors: [], cases: [] }
try {
  server = await createServer({ configFile: false, root: directory, plugins: [vue(), tailwind()],
    resolve: { alias: { '@': join(frontend, 'src') } },
    server: { host: '127.0.0.1', port: 0, fs: { allow: [directory, frontend] } }, logLevel: 'warn' })
  await server.listen()
  const port = server.httpServer.address().port
  browser = await chromium.launch({ headless: true })
  for (const width of [1360, 375]) for (const theme of ['light', 'dark']) for (const accent of ['clay', 'kraft', 'sky']) {
    const page = await browser.newPage({ viewport: { width, height: 1080 }, reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai', locale: 'zh-CN' })
    page.on('pageerror', error => report.errors.push(error.message))
    await page.goto(`http://127.0.0.1:${port}/?theme=${theme}&accent=${accent}`)
    await page.getByText('已接续 2 次配置发布', { exact: false }).waitFor()
    await page.evaluate(() => document.fonts.ready)
    const layout = await page.evaluate(() => ({ viewport: innerWidth, width: document.documentElement.scrollWidth }))
    assert.ok(layout.width <= width, `Overflow: ${JSON.stringify({ width, theme, accent, layout })}`)
    assert.equal(await page.locator('.oxidns-overview > div.grid').evaluate(el => getComputedStyle(el).display), 'grid')
    const clients = page.getByRole('region', { name: '客户端排行', exact: true })
    assert.match(await clients.innerText(), /客厅 NAS/)
    assert.match(await clients.innerText(), /书房工作电脑（有线网卡 · IPv6）/)
    assert.match(await clients.innerText(), /2001:db8:0:0:abcd:ef01:2345:6789/)
    assert.equal((await clients.locator('tbody tr').nth(2).locator('td').nth(1).innerText()).trim(), '192.0.2.12')
    assert.equal(await clients.evaluate(el => el.scrollWidth <= el.clientWidth), true)
    await page.getByRole('button', { name: '统计周期' }).click()
    const help = page.locator('[data-field-help-content]').filter({ hasText: '配置发布后继续累计' })
    await help.waitFor()
    await page.keyboard.press('Escape')
    await help.waitFor({ state: 'hidden' })
    const name = `${width}-${theme}-${accent}.png`
    await page.screenshot({ path: join(output, name), fullPage: true })
    if (width === 1360 && theme === 'light' && accent === 'clay') {
      await page.screenshot({ path: resolve(root, 'doc/img/guide/oxidns-publication-statistics.png'), fullPage: true })
      await clients.locator('..').screenshot({ path: resolve(root, 'doc/img/guide/oxidns-client-ranking-notes.png') })
    }
    report.cases.push({ width, theme, accent, ...layout })
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
