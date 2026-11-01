// Run via rt on the test server: actual component, fixture API, no production writes.
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
const directory = await mkdtemp(join(tmpdir(), 'configflow-task-visibility-'))
const output = resolve(root, '.ci-results/scheduled-task-visibility')
await mkdir(output, { recursive: true })
await symlink(join(frontend, 'node_modules'), join(directory, 'node_modules'))
await writeFile(join(directory, 'index.html'), '<!doctype html><html lang="zh-CN" data-motion="off"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>定时任务筛选示例</title><div id="app"></div><script type="module" src="/entry.js"></script></html>')
await writeFile(join(directory, 'style.css'), `@import '${frontend}/src/styles/theme.css';\n@import '${frontend}/src/styles/tokens.css';\n@source '${frontend}/src';\n`)
await writeFile(join(directory, 'entry.js'), `
import { createApp, h } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import Tasks from '@/components/settings/ScheduledTasks.vue'
import './style.css'
const params = new URLSearchParams(location.search)
document.documentElement.dataset.theme = params.get('theme') || 'light'
document.documentElement.dataset.accent = params.get('accent') || 'clay'
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:path(.*)*', component: { render: () => null } }] })
await router.push('/?' + params.toString())
createApp({ render: () => h('main', { class: 'mx-auto max-w-[1360px] space-y-5 p-3 sm:p-6' }, [
  h('header', {}, [h('h1', { class: 'm-0 font-display text-3xl' }, '系统设置'),
    h('p', { class: 'mt-1 text-xs text-muted-foreground' }, '定时任务 · 示例数据')]), h(Tasks)
]) }).use(router).mount('#app')
`)
const ago = minutes => new Date(Date.now() - minutes * 60000).toISOString()
const base = {
  key: 'domain_discovery:default', category: 'domain_discovery', name: '域名发现自动探测', detail: '',
  resource_id: 'default', profile_id: 'default', profile_name: 'Default', enabled: true,
  interval_seconds: 600, schedule_text: '', editable: false, can_run: true, run_hint: '', running: false,
  next_run_at: ago(-5), state_message: '', link: '/domain-discovery', last_run_at: ago(5),
  last_status: 'success', last_error: '', last_duration_ms: 2300, last_trigger: 'schedule',
  last_success_at: ago(5), run_count: 1158, failure_count: 0, cache_retained: null, cache_updated_at: null,
}
const fixtures = [base,
  { ...base, key: 'subscription:feed', category: 'subscription', name: '家庭订阅', profile_name: null,
    interval_seconds: 86400, last_status: 'failed', last_error: '连接上游超时', failure_count: 1, cache_retained: true, cache_updated_at: ago(60) },
  { ...base, key: 'domain_discovery:test', profile_name: '测试空间', enabled: false, can_run: false,
    next_run_at: null, run_hint: '自动探测未开启', last_status: 'skipped', last_duration_ms: 3, run_count: 0,
    last_error: '没有开启域名发现的在线 Agent（需要 1.5.0-go 或更新版本）' },
  { ...base, key: 'rule_source:manual', category: 'rule_source', name: '手动更新规则集', profile_name: null,
    enabled: false, interval_seconds: 0, next_run_at: null, link: '/rule-library' },
]
let server, browser
const report = { fixture: true, errors: [], cases: [] }
try {
  server = await createServer({ configFile: false, root: directory, plugins: [vue(), tailwind()],
    resolve: { alias: { '@': join(frontend, 'src') } },
    server: { host: '127.0.0.1', port: 0, fs: { allow: [directory, frontend] } }, logLevel: 'warn' })
  await server.listen()
  browser = await chromium.launch({ headless: true })
  const url = `http://127.0.0.1:${server.httpServer.address().port}`
  for (const width of [1360, 375]) for (const theme of ['light', 'dark']) for (const accent of ['clay', 'kraft', 'sky']) {
    const page = await browser.newPage({ viewport: { width, height: 1000 }, reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai', locale: 'zh-CN' })
    page.on('pageerror', error => report.errors.push(error.message))
    let rows = structuredClone(fixtures), reads = 0
    await page.route('**/api/tasks', route => {
      assert.equal(route.request().method(), 'GET')
      reads++
      return route.fulfill({ json: { success: true, tasks: rows } })
    })
    await page.goto(`${url}/?theme=${theme}&accent=${accent}`)
    await page.locator('[data-task="domain_discovery:default"]').waitFor()
    await page.evaluate(() => document.fonts.ready)
    const checkbox = page.getByRole('checkbox', { name: '显示已停用' })
    assert.equal(await checkbox.isChecked(), false)
    assert.equal(await page.locator('[data-task]').count(), 2)
    if (width === 1360 && theme === 'light' && accent === 'clay') {
      await page.getByRole('main').screenshot({ path: resolve(root, 'doc/img/guide/scheduled-tasks-active.png') })
    }
    await checkbox.check()
    await page.locator('[data-task="domain_discovery:test"]').waitFor()
    assert.equal(await page.locator('[data-task]').count(), 4)
    assert.equal(await page.locator('[data-task="domain_discovery:test"] [data-label="运行方式"]').innerText(), '已关闭')
    assert.equal(await page.locator('[data-task="rule_source:manual"] [data-label="运行方式"]').innerText(), '已关闭')
    assert.equal(await page.locator('[data-task="domain_discovery:test"] button[aria-label^="立即运行"]').isDisabled(), true)
    assert.equal(await page.locator('[data-task="rule_source:manual"] button[aria-label^="立即运行"]').isDisabled(), false)
    assert.equal((await page.getByRole('button', { name: /^全部 / }).innerText()).replace(/\s+/g, ' '), '全部 4')
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    assert.ok(await checkbox.locator('..').evaluate(el => el.getBoundingClientRect().height >= 44))
    await page.screenshot({ path: join(output, `${width}-${theme}-${accent}.png`), fullPage: true })
    if (width === 1360 && theme === 'light' && accent === 'clay') {
      await page.getByRole('main').screenshot({ path: resolve(root, 'doc/img/guide/scheduled-tasks-disabled.png') })
    }
    await page.getByRole('button', { name: /^规则集更新 / }).click()
    assert.equal(await page.locator('[data-task]').count(), 1)
    await checkbox.uncheck()
    await page.locator('[data-task="domain_discovery:default"]').waitFor()
    assert.equal(await page.locator('[data-task]').count(), 2)
    assert.equal(reads, 1, 'Display filters must not fetch or execute tasks')
    if (width === 375 && theme === 'dark' && accent === 'sky') {
      rows = rows.map(row => ({ ...row, enabled: false, next_run_at: null }))
      await page.getByRole('button', { name: '刷新任务列表' }).click()
      await page.getByText('没有已启用的任务', { exact: true }).waitFor()
      await checkbox.check()
      await page.locator('[data-task="domain_discovery:test"]').waitFor()
      assert.equal(await page.locator('[data-task]').count(), 4)
      await checkbox.uncheck()
      rows[0].running = true
      await page.getByRole('button', { name: '刷新任务列表' }).click()
      await page.locator('[data-task="domain_discovery:default"]').waitFor()
      assert.equal(await page.locator('[data-task]').count(), 1)
      assert.equal(await page.locator('[data-task-status]').innerText(), '运行中')
      rows = structuredClone(fixtures)
      await page.goto(`${url}/?theme=${theme}&accent=${accent}&task=domain_discovery%3Atest`)
      await page.locator('[data-task="domain_discovery:test"][data-notification-focus]').waitFor()
      assert.equal(await page.getByRole('checkbox', { name: '显示已停用' }).isChecked(), true)
      assert.equal(await page.locator('[data-task]').count(), 4)
    }
    report.cases.push({ width, theme, accent })
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
