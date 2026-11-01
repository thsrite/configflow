// Test-server-only integration: real notification APIs, isolated data, controlled router reports.
// rt --back doc/img/guide/notifications-dark.png --back doc/img/guide/notifications-light.png --back doc/img/guide/notifications-mobile.png -- bash -lc 'cf-test-remote typecheck && node scripts/tests/notification_center_browser.mjs'
import assert from 'node:assert/strict'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { networkInterfaces, tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run on the test server')
const root = process.cwd()
const temporary = await mkdtemp(join(tmpdir(), 'notification-center-'))
const apiPort = Number(process.env.CF_NOTIFICATION_API_PORT || 15762)
const webPort = Number(process.env.CF_NOTIFICATION_WEB_PORT || 15763)
const apiBase = `http://127.0.0.1:${apiPort}`
const base = `http://127.0.0.1:${webPort}`
let backend, vite, browser, logs = ''
const errors = []
const launcher = `from pathlib import Path
import os, copy
from datetime import datetime, timezone
from flask import jsonify, request
from backend.test_qa_integrity import make_app
from backend.common.config import get_config, reset_config_context
from backend import notifications
from backend.utils import task_status, webdav_backup
from backend.routes import routers
from backend.routers.base import RouterState
app, repo = make_app(Path(os.environ['DATA_DIR']))
webdav_backup.configuration_committed = lambda *args: None
repo.update_profile('default', {'name': '家庭配置'})
repo.create_profile({'id': 'office', 'name': '办公配置'})
stamp = datetime.now(timezone.utc).isoformat()
source = {'id': 'feed', 'name': '常用订阅', 'url': 'https://notification-demo.invalid/sub', 'enabled': True, 'interval': 3600}
rule = {'id': 'rule', 'name': '视频分流规则', 'url': 'https://notification-demo.invalid/rule', 'enabled': True, 'source_type': 'url', 'behavior': 'domain', 'format': 'text', 'interval': 3600}
repo.save_shared({'subscriptions': [source], 'nodes': [], 'rule_library': [rule], 'subscription_aggregations': []})
deployment = {'deployment_id': 'demo-failed', 'status': 'failed', 'profile_id': 'default', 'service_type': 'mihomo', 'created_at': stamp, 'updated_at': stamp,
              'error': '配置校验未通过', 'error_details': 'rules.yaml 第 12 行：规则目标不存在', 'failed_stage': 'verifying'}
agent = {'id': 'gateway', 'name': '客厅网关', 'host': '192.0.2.4', 'port': 9100, 'token': 'demo-token', 'enabled': True,
         'profile_id': 'default', 'service_type': 'mihomo', 'deployment_method': 'shell', 'version': '1.8.43-go',
         'latest_deployment': deployment, 'deployments': {'demo-failed': deployment},
         'rule_sync': {'settings': {'library': True, 'rules': False}, 'state': {'status': 'blocked', 'message': '请先成功发布配置，建立同步基准'}}}
oxi = {'id': 'dns', 'name': '家庭 DNS', 'host': '192.0.2.5', 'port': 9100, 'enabled': True, 'profile_id': 'default', 'service_type': 'oxidns',
       'deployment_method': 'shell', 'version': '1.8.43-go', 'token': 'demo-oxi',
       'dns_follow': {'source_agent_id': 'gateway', 'upstream_tag': 'upstream', 'upstream_index': 0, 'connection': 'network', 'address': '192.0.2.4'},
       'dns_follow_state': {'status': 'failed', 'message': '上游地址已变化，请重新确认跟随设置'}}
repo.update_system_transaction(lambda system: system.update(agents=[agent, oxi], backup={'auto_backup': True, 'webdav_url': 'https://backup.invalid', 'webdav_username': 'demo', 'webdav_password': 'demo-password'}))
router = {'id': 'office-router', 'name': '办公主路由', 'type': 'routeros', 'endpoint': 'http://192.0.2.1', 'username': 'demo', 'password': 'demo',
          'proxy_gateway_agent_id': '', 'dns_agent_id': '', 'ip_rule_sets': [], 'auto_rule_sync': True}
repo.update_profile_transaction('office', lambda config: config.update(routers={'items': [router]}))
report = {'in_sync': False, 'summary': {'missing': 1},
          'desired': {'fake_ip_range': '198.18.0.0/16', 'proxy_gateway': '192.0.2.4', 'proxy_cidrs': [], 'warnings': []},
          'routes': {'items': [{'cidr': '198.18.0.0/16', 'sources': ['fake-ip'], 'status': 'missing', 'detail': ''}], 'extra': [], 'unmanaged_other': [],
                     'forward_hint': {'status': 'drop', 'warning': '防火墙阻止客户端流量转给 Mihomo，需要放行同口转发。', 'fix_command': '/ip firewall filter add chain=forward action=accept comment="demo"'}},
          'nat_bypass': {'items': [], 'disabled_rules': []}, 'return_routing': {'status': 'disabled', 'message': '未启用自动配置回流。'}}
plan = {'stages': [{'key': 'routes', 'title': '主路由代理路由', 'ops': [{'kind': 'route_add', 'summary': '添加 198.18.0.0/16'}]}],
        'blockers': [], 'warnings': [], 'fingerprint': 'notification-demo', 'empty': False}
class Driver:
    def close(self): pass
routers._driver = lambda item: Driver()
routers._inspect = lambda *args: (None, {}, RouterState(), copy.deepcopy(report))
routers.build_plan = lambda *args: copy.deepcopy(plan)
notifications.record_router('office', get_config('office'), router, report, plan)
task_status.record('subscription:feed', ok=False, error='请求超时', cache_retained=True, cache_updated_at=stamp)
task_status.record('rule_source:rule', ok=False, error='规则内容校验失败：第 12 行格式错误', cache_retained=False)
task_status.record('webdav_backup', ok=False, error='备份服务器返回 HTTP 401')
notifications.record_agent(agent, repo)
mutations = []
@app.before_request
def audit():
    if request.path.startswith('/api/') and request.method not in ('GET', 'HEAD', 'OPTIONS') and request.path not in ('/api/notifications/read', '/api/routers/office-router/plan'):
        mutations.append(request.path)
        return jsonify(success=False, message='unexpected mutation'), 409
@app.get('/audit/state')
def state(): return jsonify(mutations=mutations)
@app.post('/audit/recover')
def recover():
    task_status.record('subscription:feed', ok=True)
    return jsonify(success=True)
@app.post('/audit/fail')
def fail():
    task_status.record('subscription:feed', ok=False, error='请求超时', cache_retained=True, cache_updated_at=stamp)
    return jsonify(success=True)
@app.post('/audit/repeat')
def repeat():
    fail()
    task_status.record('webdav_backup', ok=False, error='备份服务器返回 HTTP 401')
    return jsonify(success=True)
@app.post('/audit/remove')
def remove():
    repo.update_shared_transaction(lambda shared: shared.update(subscriptions=[]))
    reset_config_context()
    return jsonify(success=True)
app.run(host='0.0.0.0', port=${apiPort}, debug=False, use_reloader=False)
`
try {
  backend = spawn('/home/sunbroke/cf-test/.venv/bin/python', ['-c', launcher], {
    cwd: root, env: { ...process.env, DATA_DIR: temporary, LOG_FILE: join(temporary, 'app.log'), ADMIN_USERNAME: '', ADMIN_PASSWORD: '' }, stdio: ['ignore', 'pipe', 'pipe'],
  })
  backend.stdout.on('data', chunk => { logs += chunk })
  backend.stderr.on('data', chunk => { logs += chunk })
  let ready = false
  for (let attempt = 0; attempt < 100; attempt++) {
    if (backend.exitCode !== null) throw new Error(logs)
    try { if ((await fetch(`${apiBase}/audit/state`)).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  assert.ok(ready, logs)
  const initial = await (await fetch(`${apiBase}/api/notifications`)).json()
  assert.ok(initial.items.length >= 6)
  const { createServer } = await import(pathToFileURL(resolve(root, 'frontend/node_modules/vite/dist/node/index.js')))
  vite = await createServer({ root: resolve(root, 'frontend'), configFile: resolve(root, 'frontend/vite.config.mts'), logLevel: 'warn',
    server: { host: '0.0.0.0', port: webPort, strictPort: true, proxy: { '/api': { target: apiBase, changeOrigin: true } } } })
  await vite.listen()
  const { chromium } = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE || '/tmp/configflow-guide-browser/node_modules/playwright/index.mjs'))
  browser = await chromium.launch({ headless: true })
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
  page.setDefaultTimeout(15000)
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/agents/dns/dns-follow', route => route.fulfill({ json: {
    success: true, binding: { source_agent_id: 'gateway', upstream_tag: 'upstream', upstream_index: 0, connection: 'network', address: '192.0.2.4' },
    state: { status: 'failed', message: '上游地址已变化，请重新确认跟随设置' },
    sources: [{ id: 'gateway', name: '客厅网关', suggested_address: '192.0.2.4' }],
    upstreams: [{ tag: 'upstream', index: 0, address: 'udp://192.0.2.4:53' }],
  } }))
  await page.addInitScript(() => localStorage.setItem('configflow-theme', 'dark'))
  await page.goto(`${base}/system-settings?tab=tasks`)
  const panel = page.locator('[data-notifications-panel]')
  const open = async () => {
    await page.locator('[data-notifications-trigger]').click()
    await panel.waitFor()
    await page.waitForFunction(() => {
      const panel = document.querySelector('[data-notifications-panel]')
      return panel?.querySelector('[data-notification]') || panel?.textContent?.includes('暂无新通知')
    })
  }
  const follow = async id => { await open(); await panel.locator(`[data-notification="${id}"]`).getByRole('button').last().click(); await panel.waitFor({ state: 'hidden' }) }
  // Sample actual frames: opening the notification drawer must leave the rest
  // of the page's brightness unchanged, including static and reduced motion.
  const motion = []
  for (const theme of ['dark', 'light']) {
    for (const mode of ['on', 'off', 'reduced']) {
      await page.emulateMedia({ reducedMotion: mode === 'reduced' ? 'reduce' : 'no-preference' })
      await page.evaluate(({ theme, mode }) => {
        document.documentElement.dataset.theme = theme
        document.documentElement.dataset.motion = mode === 'off' ? 'off' : 'on'
      }, { theme, mode })
      await page.waitForTimeout(250)
      const frames = await page.evaluate(() => new Promise(resolve => {
        const frames = []
        const start = performance.now()
        document.querySelector('[data-notifications-trigger]').click()
        const sample = time => {
          const panel = document.querySelector('[data-notifications-panel]')
          const overlay = document.querySelector('[data-slot="sheet-overlay"]')
          if (panel && overlay) {
            const style = getComputedStyle(panel)
            const box = panel.getBoundingClientRect()
            frames.push({ at: time - start, opacity: Number(style.opacity), travel: box.right - innerWidth,
              duration: parseFloat(style.animationDuration), overlay: getComputedStyle(overlay).backgroundColor })
          }
          if (time - start < 400) requestAnimationFrame(sample)
          else resolve(frames)
        }
        requestAnimationFrame(sample)
      }))
      assert.ok(frames.length > 1, `${theme}/${mode}: no frames captured`)
      assert.ok(frames.every(frame => frame.overlay === 'rgba(0, 0, 0, 0)'), `${theme}/${mode}: background dimmed`)
      assert.ok(frames.every(frame => frame.travel >= -1 && frame.travel <= 16), `${theme}/${mode}: full-width slide`)
      if (mode === 'on') {
        assert.ok(frames.some(frame => frame.opacity > 0 && frame.opacity < 1), `${theme}: missing gradual entrance`)
        assert.ok(frames.every(frame => frame.duration <= 0.25), `${theme}: entrance too slow`)
      } else assert.ok(frames.every(frame => frame.duration < 0.001), `${theme}/${mode}: ignored motion preference`)
      assert.equal(frames.at(-1).opacity, 1)
      motion.push({ theme, mode, frames })
      await page.keyboard.press('Escape')
      await panel.waitFor({ state: 'hidden' })
      await page.waitForFunction(() => document.activeElement?.matches('[data-notifications-trigger]'))
      assert.equal(await page.locator('[data-slot="sheet-overlay"]').count(), 0, 'overlay survived close')
    }
  }
  await mkdir(resolve(root, '.ci-results/notification-motion'), { recursive: true })
  await writeFile(resolve(root, '.ci-results/notification-motion/frames.json'), JSON.stringify(motion, null, 2))
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await open()
  const badge = await page.locator('[data-notification-count]').textContent()
  await mkdir(resolve(root, 'doc/img/guide'), { recursive: true })
  for (const theme of ['dark', 'light']) {
    await page.evaluate(theme => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.motion = 'off' }, theme)
    await page.waitForTimeout(600) // Let theme transitions settle before documenting contrast.
    await page.screenshot({ path: resolve(root, `doc/img/guide/notifications-${theme}.png`) })
  }
  await page.setViewportSize({ width: 375, height: 812 })
  await page.evaluate(() => { document.documentElement.dataset.theme = 'dark' })
  await page.waitForTimeout(600)
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile overflow')
  assert.ok(await panel.evaluate(node => node.scrollWidth <= node.clientWidth), 'notification panel overflow')
  await page.screenshot({ path: resolve(root, 'doc/img/guide/notifications-mobile.png') })
  await page.keyboard.press('Escape')
  await panel.waitFor({ state: 'hidden' })
  await page.setViewportSize({ width: 1280, height: 900 })

  await page.evaluate(() => { document.documentElement.dataset.motion = 'on' })
  // A destination dialog must not mount under the drawer's exit animation.
  await page.evaluate(() => {
    window.notificationOverlap = false
    window.notificationObserver = new MutationObserver(() => {
      if (document.querySelector('[data-notifications-panel]') && document.querySelector('[data-slot="dialog-content"][data-state="open"]')) {
        window.notificationOverlap = true
      }
    })
    window.notificationObserver.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-state'] })
  })
  await follow('task:subscription:feed')
  await page.getByRole('heading', { name: '编辑订阅', exact: true }).waitFor()
  assert.equal(await page.evaluate(() => { window.notificationObserver.disconnect(); return window.notificationOverlap }), false, 'drawer overlapped destination dialog')
  assert.equal(await page.locator('#sub-name').inputValue(), '常用订阅')
  await page.locator('#sub-url[data-notification-focus]').waitFor()
  await page.waitForFunction(() => document.activeElement?.id === 'sub-url')
  await page.getByRole('button', { name: '取消', exact: true }).click()

  await follow('task:rule_source:rule')
  await page.locator('#lib-url[data-notification-focus]').waitFor()
  await page.waitForFunction(() => document.activeElement?.id === 'lib-url')
  assert.equal(await page.locator('#lib-url').inputValue(), 'https://notification-demo.invalid/rule')
  await page.getByRole('button', { name: '取消', exact: true }).click()

  await follow('deployment:gateway')
  await page.locator('[data-deployment-record="demo-failed"][data-notification-focus]').waitFor()
  await page.locator('[data-deployment-record="demo-failed"] details[open]').waitFor()
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).last().click()

  await follow('task:agent_rule_sync:gateway')
  await page.locator('[data-notification-agent="gateway"] [aria-label="规则同步"][data-notification-focus]').waitFor()

  await follow('task:dns_follow:dns')
  await page.getByRole('heading', { name: '跟随 Mihomo DNS', exact: true }).waitFor()
  assert.equal(await page.locator('#dns-follow-address-dns').inputValue(), '192.0.2.4')
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).last().click()

  await follow('task:router_rule_sync:office:office-router')
  await page.locator('[data-router-rule-sync][data-notification-focus]').waitFor()

  await follow('router:office:office-router:firewall')
  await page.locator('[data-router-ipv4-forward-hint][data-notification-focus]').waitFor()
  assert.equal(await page.evaluate(() => sessionStorage.getItem('configflow.activeProfile')), 'office')
  assert.match(await page.locator('[data-router-report]').innerText(), /办公主路由/)

  await follow('router:office:office-router:changes')
  await page.getByRole('dialog').filter({ hasText: '发布到 办公主路由' }).waitFor()
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()

  await follow('task:webdav_backup')
  await page.locator('#webdav-url[data-notification-focus]').waitFor()
  await page.waitForFunction(() => document.activeElement?.id === 'webdav-url')
  assert.equal(await page.locator('#webdav-url').inputValue(), 'https://backup.invalid')
  await page.getByRole('button', { name: '取消', exact: true }).click()

  await fetch(`${apiBase}/audit/fail`, { method: 'POST' })
  await open()
  const subscriptionNotice = panel.locator('[data-notification="task:subscription:feed"]')
  await subscriptionNotice.waitFor()
  await subscriptionNotice.getByRole('button', { name: '标为已读', exact: true }).click()
  await subscriptionNotice.waitFor({ state: 'hidden' })
  await page.reload()
  await open()
  assert.equal(await subscriptionNotice.count(), 0, 'read receipt must remain cleared after reload')
  await fetch(`${apiBase}/audit/fail`, { method: 'POST' })
  await panel.getByRole('button', { name: '刷新通知', exact: true }).click()
  await subscriptionNotice.waitFor()
  await fetch(`${apiBase}/audit/recover`, { method: 'POST' })
  await panel.getByRole('button', { name: '刷新通知', exact: true }).click()
  await subscriptionNotice.waitFor({ state: 'hidden' })
  assert.equal(await panel.getByRole('button', { name: /已恢复/ }).count(), 0)
  await fetch(`${apiBase}/audit/repeat`, { method: 'POST' })
  await panel.getByRole('button', { name: '刷新通知', exact: true }).click()
  await subscriptionNotice.waitFor()
  await panel.getByRole('button', { name: '全部已读', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[data-notifications-panel] [data-notification]'))
  assert.equal(await page.locator('[data-notification-count]').count(), 0)
  await page.reload()
  await open()
  assert.equal(await panel.locator('[data-notification]').count(), 0)
  await page.keyboard.press('Escape')
  await panel.waitFor({ state: 'hidden' })
  await fetch(`${apiBase}/audit/remove`, { method: 'POST' })
  await page.goto(`${base}/subscriptions?subscription=feed&section=settings`)
  await page.getByText('对应订阅不存在或已删除。', { exact: true }).waitFor()
  assert.equal(await page.locator('#sub-url').count(), 0)
  assert.deepEqual((await (await fetch(`${apiBase}/audit/state`)).json()).mutations, [])
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ initial: Number(badge), read_clears: true, read_survives_reload: true, new_failure: true, recovery_clears: true, destinations: ['subscription', 'rule', 'deployment', 'agent-rule-sync', 'dns-follow', 'router-rule-sync', 'router-firewall', 'router-preview', 'webdav'], cross_profile: true, missing_target: true, mobile: true, mutations: 0, browser_errors: errors }))
} catch (error) {
  console.error(logs.slice(-3500))
  throw error
} finally {
  await browser?.close()
  await vite?.close()
  if (backend && backend.exitCode === null) {
    backend.kill('SIGTERM')
    await new Promise(resolve => backend.once('exit', resolve))
  }
  await rm(temporary, { recursive: true, force: true })
}
