// IPv6 acceptance results in the real Vue components with a mocked API: the
// steps card and the device check show 「IPv6 网络」 and, with a test client,
// 「客户端分流」 — no node test — and 「补齐缺少的配置」 only when the host misses
// a setting it writes. Also captures the documentation screenshots.
// Run on the test server after frontend dependencies are installed (cf-test typecheck):
//   rt -e CF_PLAYWRIGHT_MODULE=<playwright/index.mjs> --back .ci-results/ipv6-verify-results -- node scripts/tests/ipv6_verify_results_browser.mjs
// No Agent, router or ConfigFlow server is contacted.
import assert from 'node:assert/strict'
import { cp, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const frontend = join(root, 'frontend')
const lockHash = createHash('sha256').update(await readFile(join(frontend, 'package-lock.json'))).digest('hex').slice(0, 16)
const dependencies = process.env.CF_NODE_MODULES || `${process.env.HOME}/cf-test/node_modules/${lockHash}/node_modules`
async function dependency(name) {
  const dir = join(dependencies, name)
  const pkg = JSON.parse(await readFile(join(dir, 'package.json'), 'utf8'))
  const entry = pkg.exports['.'].import || pkg.exports['.'].default
  return import(join(dir, typeof entry === 'string' ? entry : entry.default))
}
const { createServer } = await dependency('vite')
const { default: vue } = await dependency('@vitejs/plugin-vue')
const { default: tailwindcss } = await dependency('@tailwindcss/vite')
const { chromium } = await import(process.env.CF_PLAYWRIGHT_MODULE)
const output = join(root, '.ci-results/ipv6-verify-results')
await mkdir(output, { recursive: true })
const scratch = await mkdtemp('/tmp/configflow-ipv6-verify-')
await cp(join(frontend, 'src'), join(scratch, 'src'), { recursive: true })
await symlink(dependencies, join(scratch, 'node_modules'))
await writeFile(join(scratch, 'index.html'), '<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app"></div><script type="module" src="/main.ts"></script></body></html>')
await writeFile(join(scratch, 'main.ts'), `import { createApp } from 'vue'; import { createRouter, createMemoryHistory } from 'vue-router'; import './src/styles/theme.css'; import './src/styles/tokens.css'; import Harness from './Harness.vue'
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:any(.*)*', component: { render: () => null } }] })
createApp(Harness).use(router).mount('#app')`)
await writeFile(join(scratch, 'Harness.vue'), `<script setup lang="ts">
import { ref } from 'vue'
import MihomoSettings from './src/components/generate/MihomoSettings.vue'
const open = ref(true)
</script><template><MihomoSettings v-model:open="open" profile-id="default" preferred-agent-id="gw" /></template>`)

const nodesYAML = 'ipv6: true\nmixed-port: 7890\nexternal-controller: 0.0.0.0:9090\ndns:\n  enable: true\n  ipv6: false\n  listen: 0.0.0.0:1053\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n  fake-ip-range6: fc00:0:0:3::1/64\ntun:\n  enable: true\n  auto-route: true\n'
const clientsYAML = nodesYAML.replace('  ipv6: false\n', '  ipv6: true\n')
const ok = (id, label, message = `${label}正常。`) => ({ id, label, status: 'ok', message })
function readiness({ publication = 'synced', deployed = null, prepared = true } = {}) {
  return { success: true, agent_id: 'gw', profile_id: 'default', config_revision: 7, checked_at: new Date().toISOString(), publication,
    configuration: { ipv6: true, dns_ipv6: false, fake_ipv6_range: 'fc00:0:0:3::1/64', errors: [] },
    host: { core: { ipv6: true } },
    checks: [
      ok('configuration', '已保存的 IPv6 设置'),
      ok('host_address', '主机 IPv6 地址'), ok('host_default_route', 'IPv6 默认路由'), ok('host_forwarding_ra', '转发与 RA'),
      prepared ? ok('host_persistence', '开机恢复配置') : { id: 'host_persistence', label: '开机恢复配置', status: 'warning', message: '部分参数未持久化。' },
      ...(deployed ? [{ id: 'deployed_config', label: '设备配置', status: 'warning', message: deployed }] : []),
    ],
    routers: [{ id: 'ros', name: '测试 ROS', status: 'ok', can_prepare: false, checks: [ok('fake_ipv6', 'IPv6 Fake-IP 路由'), ok('wan', 'IPv6 外网路由')] }],
    can_prepare: true, prepare_interface: 'eth0', check_token: 'a'.repeat(64),
    connectivity: { status: 'not_checked', message: '' } }
}
const runtime = [ok('runtime_deployed_config', '已发布配置'), ok('runtime_runtime_core', 'Mihomo 运行状态'), ok('runtime_runtime_ipv6', '运行中的 IPv6 开关'),
  ok('runtime_runtime_tun', '运行中的 TUN'), ok('runtime_dns_a', 'DNS A'), ok('runtime_dns_aaaa', 'DNS AAAA')]
const client = (transport = 'ipv6') => [ok('client_http_ipv4', '客户端访问'), ok('client_mihomo_path', '客户端经过 Mihomo'), ok('client_ros_path', '客户端经过 ROS'),
  transport === 'ipv6' ? ok('client_ipv6_transport', '客户端连接使用 IPv6 节点', '本次请求经由「🪜自建 IPv6 节点 → us-ipv6-a」，节点连接使用 IPv6')
    : { id: 'client_ipv6_transport', label: '客户端连接使用 IPv6 节点', status: 'unknown', message: '本次请求经由「🚀 节点选择 → hk-ipv4-b」，节点连接使用 IPv4。要验证 IPv6 节点，请在「验收设置」把检测网址换成会分到 IPv6 节点的网站' }]
const verification = (checks, scope = 'client_path') => ({ status: checks.every(check => check.status === 'ok') ? 'ok' : 'warning', scope, config_revision: 7, checked_at: new Date().toISOString(), checks })
const hostOnly = verification([...runtime, { id: 'client_not_configured', label: '客户端分流', status: 'unknown', message: '未配置检测客户端' }], 'mihomo_runtime')
// Older Agents still report their own node test: it must not show anywhere.
const legacyNodeTest = [{ id: 'runtime_proxy_http', label: '代理访问网站', status: 'error', message: '没有通过这个节点成功访问测试网站' },
  { id: 'runtime_node_ipv6_transport', label: 'Mihomo IPv6 连接', status: 'unknown', message: '尚未观察到能确认由 Mihomo 发起的节点 IPv6 连接' }]

const scenarios = [
  { name: 'mihomo-ipv6-mode-steps', theme: 'light', width: 1440, yaml: clientsYAML, readiness: readiness({ publication: 'changed', deployed: '已保存草稿与设备配置不同：DNS IPv6。请通过保存并发布应用配置。' }), upstream: 'pending', verification: null, shot: 'page',
    expect: { verify: [['IPv6 网络', '待验证'], ['客户端分流', '待验证']] } },
  { name: 'mihomo-ipv6-mode-nodes', theme: 'light', width: 1440, yaml: nodesYAML, readiness: readiness({ publication: 'changed', deployed: '已保存草稿与设备配置不同：IPv6 Fake-IP 网段。请通过保存并发布应用配置。' }), upstream: 'pending', verification: null, shot: 'page',
    expect: { verify: [['IPv6 网络', '待验证'], ['客户端分流', '待验证']] } },
  { name: 'mihomo-ipv6-steps-after-router', theme: 'dark', width: 1214, yaml: nodesYAML, readiness: readiness(), upstream: 'synced', verification: verification([...runtime, ...client()]), clientAgent: 'client', shot: 'card',
    expect: { verify: [['IPv6 网络', '可用'], ['客户端分流', '已通过']], progress: '2 / 2 通过' } },
  { name: 'mihomo-ipv6-pending-after-change', theme: 'dark', width: 1214, yaml: nodesYAML, readiness: readiness({ publication: 'changed' }), upstream: 'synced', verification: null, shot: 'card',
    expect: { verify: [['IPv6 网络', '待验证'], ['客户端分流', '待验证']] } },
  { name: 'mihomo-ipv6-three-results', theme: 'light', width: 1440, yaml: nodesYAML, readiness: readiness(), upstream: 'synced', verification: verification([...hostOnly.checks, ...legacyNodeTest], 'mihomo_runtime'), shot: 'dialog',
    expect: { dialog: [['IPv6 网络', '已就绪']], prepare: false, verify: [['IPv6 网络', '已通过']], progress: '1 / 1 通过' } },
  { name: 'mihomo-ipv6-verify-after-router', theme: 'dark', width: 1440, yaml: nodesYAML, readiness: readiness(), upstream: 'synced', verification: verification([...runtime, ...client()]), clientAgent: 'client', shot: 'dialog',
    expect: { dialog: [['IPv6 网络', '可用'], ['客户端分流', '已通过']], prepare: false } },
  { name: 'mihomo-ipv6-client-ipv4-node', theme: 'light', width: 1440, yaml: nodesYAML, readiness: readiness({ prepared: false }), upstream: 'synced', verification: verification([...runtime, ...client('ipv4')]), clientAgent: 'client', shot: 'dialog',
    expect: { dialog: [['IPv6 网络', '需处理'], ['客户端分流', '待验证']], prepare: true, text: '节点连接使用 IPv4' } },
]

const report = { mockedAPI: true, scenarios: [], pageErrors: [] }
let browser
let server
let activePage
try {
  server = await createServer({ configFile: false, root: scratch, plugins: [vue(), tailwindcss()],
    resolve: { alias: { '@': join(scratch, 'src') } },
    server: { host: '127.0.0.1', port: 0, strictPort: false, watch: null, fs: { allow: [scratch, dependencies] } },
    cacheDir: join(scratch, '.vite'), logLevel: 'error' })
  await server.listen()
  const base = `http://127.0.0.1:${server.httpServer.address().port}`
  browser = await chromium.launch({ headless: true })
  for (const scenario of scenarios) {
    const context = await browser.newContext({ viewport: { width: scenario.width, height: 1000 }, colorScheme: scenario.theme, reducedMotion: 'reduce', deviceScaleFactor: 1 })
    const page = await context.newPage()
    activePage = page
    page.on('pageerror', error => report.pageErrors.push(`${scenario.name}: ${error}`))
    await page.addInitScript(theme => {
      const apply = () => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.accent = 'clay'; document.documentElement.dataset.motion = 'off' }
      if (document.documentElement) apply()
      else document.addEventListener('DOMContentLoaded', apply, { once: true })
    }, scenario.theme)
    const writes = []
    await page.route(url => new URL(url).pathname.startsWith('/api/'), async route => {
      const request = route.request()
      const url = new URL(request.url())
      let data = {}
      if (url.pathname === '/api/custom-config/mihomo') data = { config: scenario.yaml, default_config: scenario.yaml, provider_update_interval: 3600 }
      else if (url.pathname === '/api/agents') data = [{ id: 'gw', name: '测试 Mihomo', service_type: 'mihomo', profile_id: 'default', status: 'online', enabled: true, version: '1.9.5-go' }]
      else if (url.pathname === '/api/profiles') data = [{ id: 'default', name: '默认配置', is_default: true }]
      else if (url.pathname === '/api/routers') data = { routers: [{ id: 'ros', name: '测试 ROS', type: 'routeros', proxy_gateway_agent_id: 'gw' }] }
      else if (url.pathname === '/api/agents/gw/upstream-routers') data = { success: true, agent_id: 'gw', agent_profile_id: 'default', checked_at: new Date().toISOString(), cached: false,
        routers: [{ id: 'ros', name: '测试 ROS', type: 'routeros', profile_id: 'default', profile_name: '默认配置', state: scenario.upstream, pending: scenario.upstream === 'pending' ? 2 : 0,
          items: scenario.upstream === 'pending' ? [{ key: 'ipv6_fakeip', label: 'IPv6 Fake-IP 分流路由', status: '待添加', count: 1 }, { key: 'ipv6_rules', label: 'IPv6 规则集路由', status: '待添加', count: 1 }] : [], message: '' }] }
      else if (url.pathname === '/api/agents/gw/ipv6-readiness') data = scenario.readiness
      else if (url.pathname === '/api/agents/gw/ipv6-readiness/verify') {
        if (request.method() !== 'GET') writes.push('verify')
        data = { success: true, agent_id: 'gw', profile_id: 'default', verification: scenario.verification }
      } else if (url.pathname === '/api/agents/gw/ipv6-readiness/verification-settings') data = { success: true, agent_id: 'gw', profile_id: 'default',
        settings: { client_agent_id: scenario.clientAgent || '', test_url: '' }, candidates: [{ id: 'client', name: '检测客户端', host: '192.0.2.30', status: 'online', supported: true }] }
      else if (url.pathname === '/api/auth/status') data = { authEnabled: false }
      else if (request.method() !== 'GET') writes.push(`${request.method()} ${url.pathname}`)
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(data) })
    })
    await page.goto(base)
    const card = page.locator('[data-ipv6-card]')
    await card.locator('[data-ipv6-step-verify]').waitFor()
    const verify = card.locator('[data-ipv6-step-verify]')
    // Wait for the saved result to replace the reading state.
    await page.waitForFunction(() => !document.querySelector('[data-ipv6-step-verify] [role="status"] .animate-spin'))
    const rows = async scope => scope.locator('[data-agent-status-row], [data-ipv6-check-group]').evaluateAll(elements => elements
      .filter(element => element.matches('[data-ipv6-check-group]'))
      .map(element => element.innerText.split('\n').map(line => line.trim()).filter(Boolean).slice(0, 2)))
    const verifyRows = await rows(verify)
    if (scenario.expect.verify) assert.deepEqual(verifyRows, scenario.expect.verify, `${scenario.name}: 验收组`)
    if (scenario.expect.progress) assert.match(await verify.innerText(), new RegExp(scenario.expect.progress))
    assert.doesNotMatch(await card.innerText(), /IPv6 节点连接|通过节点访问检测网站失败|尚未观察到/, `${scenario.name}: no node outcome`)
    const entry = { name: scenario.name, verify: verifyRows }
    if (scenario.shot === 'dialog') {
      await card.getByRole('button', { name: '检查设备 IPv6', exact: true }).click()
      const dialog = page.locator('[data-mihomo-ipv6-dialog]')
      const summary = dialog.locator('[data-ipv6-check-summary]')
      await summary.waitFor()
      await page.waitForFunction(() => !document.querySelector('[data-mihomo-ipv6-dialog] .animate-spin'))
      const dialogRows = await rows(summary)
      assert.deepEqual(dialogRows, scenario.expect.dialog, `${scenario.name}: 检查窗口`)
      assert.equal(await dialog.getByRole('button', { name: '补齐缺少的配置', exact: true }).count(), scenario.expect.prepare ? 1 : 0, `${scenario.name}: 补齐按钮`)
      assert.doesNotMatch(await dialog.innerText(), /IPv6 节点连接|未配置检测客户端|通过节点访问检测网站失败/)
      if (scenario.expect.text) assert.match(await dialog.innerText(), new RegExp(scenario.expect.text))
      entry.dialog = dialogRows
      await dialog.screenshot({ path: join(output, `${scenario.name}.png`) })
    } else if (scenario.shot === 'card') {
      await card.screenshot({ path: join(output, `${scenario.name}.png`) })
    } else {
      await page.evaluate(() => (document.activeElement)?.blur())
      await page.screenshot({ path: join(output, `${scenario.name}.png`) })
    }
    assert.deepEqual(writes, [], `${scenario.name}: read only`)
    report.scenarios.push(entry)
    await context.close()
  }
  assert.deepEqual(report.pageErrors, [])
} catch (failure) {
  report.failure = String(failure)
  if (activePage && !activePage.isClosed()) {
    report.body = await activePage.locator('body').innerText()
    await activePage.screenshot({ path: join(output, 'failed-run.png') })
  }
  throw failure
} finally {
  await browser?.close()
  await server?.close()
  await writeFile(join(output, 'report.json'), JSON.stringify(report, null, 2) + '\n')
  await rm(scratch, { recursive: true, force: true })
}
console.log(JSON.stringify({ scenarios: report.scenarios.length, pageErrors: report.pageErrors.length, output }))
