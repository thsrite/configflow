// Test-server-only browser regression and documentation capture, with a temporary repository.
// cf-test typecheck
// rt --no-sync --back doc/img/guide/aggregation-subscription-cache.png -- node scripts/tests/aggregation_cache_preview_browser.mjs
import assert from 'node:assert/strict'
import { mkdtemp, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { networkInterfaces, tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { pathToFileURL } from 'node:url'

assert.ok(Object.values(networkInterfaces()).flat().some(item => item?.address === '192.168.0.32'), 'Run on the test server')
const root = process.cwd()
const temporary = await mkdtemp(join(tmpdir(), 'aggregation-cache-preview-'))
const apiPort = Number(process.env.CF_CACHE_API_PORT || 15742)
const webPort = Number(process.env.CF_CACHE_WEB_PORT || 15743)
const apiBase = `http://127.0.0.1:${apiPort}`
const playwright = process.env.PLAYWRIGHT_MODULE || '/tmp/configflow-guide-browser/node_modules/playwright/index.mjs'
let backend, vite, browser, logs = ''
const errors = [], downloads = []
const launcher = `from pathlib import Path
import os
from flask import jsonify
from backend.test_qa_integrity import make_app
from backend.utils.subscription_cache import save_subscription_nodes, delete_subscription_cache, load_subscription_cache
from backend.utils import subscription_health, task_status
from backend.routes import subscriptions, aggregations
from backend.utils import sub_store_client
app, repo = make_app(Path(os.environ['DATA_DIR']))
source = {'id': 's', 'name': '演示订阅', 'url': 'https://cache-preview.invalid/sub', 'enabled': True, 'interval': 0}
repo.save_shared({'subscriptions': [source], 'nodes': [], 'rule_library': [],
    'subscription_aggregations': [{'id': 'a', 'name': '家庭常用', 'subscriptions': ['s'], 'nodes': []}]})
repo.save_profile('default', {'proxy_groups': [{'id': 'g', 'name': 'Proxy', 'type': 'select', 'aggregations': ['a']}], 'rule_configs': []})
def seed(names):
    save_subscription_nodes('s', [{'id': str(i), 'name': name, 'type': 'ss', 'server': '192.0.2.1', 'port': 443,
        'params': {'cipher': 'aes-128-gcm', 'password': 'demo-only'}} for i, name in enumerate(names)], {'url': source['url']})
seed(['香港 · 缓存节点', '日本 · 缓存节点'])
subscription_health.record_fetch('s', ok=True, count=2)
task_status.record('subscription:s', ok=True, trigger='manual')
before = (subscription_health.load_health(), task_status.load())
attempts = []
def forbidden(*args, **kwargs):
    attempts.append('unexpected source download')
    raise RuntimeError('preview must never download')
sub_store_client.get_subscription_proxies_yaml = forbidden
subscriptions.get_subscription_proxies_yaml = forbidden
aggregations.get_subscription_proxies_yaml = forbidden
@app.route('/audit/state')
def state():
    return jsonify(attempts=attempts, history_unchanged=before == (subscription_health.load_health(), task_status.load()))
@app.route('/audit/update', methods=['POST'])
def update():
    seed(['来源更新后的节点'])
    return jsonify(success=True)
@app.route('/audit/clear', methods=['POST'])
def clear():
    delete_subscription_cache('s')
    return jsonify(success=True)
app.test_client().post('/api/settings/subscription-aggregation', json={'enabled': True})
app.run(host='0.0.0.0', port=${apiPort}, debug=False, use_reloader=False)
`
try {
  backend = spawn('/home/sunbroke/cf-test/.venv/bin/python', ['-c', launcher], {
    cwd: root, env: { ...process.env, DATA_DIR: temporary, LOG_FILE: join(temporary, 'app.log'), ADMIN_USERNAME: '', ADMIN_PASSWORD: '' },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  backend.stdout.on('data', data => { logs += data })
  backend.stderr.on('data', data => { logs += data })
  let ready = false
  for (let attempt = 0; attempt < 100; attempt++) {
    if (backend.exitCode !== null) throw new Error(logs)
    try { if ((await fetch(`${apiBase}/audit/state`)).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  assert.ok(ready, logs)
  const { createServer } = await import(pathToFileURL(resolve(root, 'frontend/node_modules/vite/dist/node/index.js')))
  vite = await createServer({ root: resolve(root, 'frontend'), configFile: resolve(root, 'frontend/vite.config.mts'), logLevel: 'warn',
    server: { host: '0.0.0.0', port: webPort, strictPort: true, proxy: { '/api': { target: apiBase, changeOrigin: true } } } })
  await vite.listen()
  const { chromium } = await import(pathToFileURL(playwright))
  browser = await chromium.launch({ headless: true })
  const page = await browser.newPage({ viewport: { width: 1280, height: 850 } })
  page.on('pageerror', error => errors.push(error.message))
  page.on('request', request => { if (/\/subscriptions\/[^/]+\/fetch/.test(request.url())) downloads.push(request.url()) })
  await page.addInitScript(() => localStorage.setItem('configflow-theme', 'light'))
  await page.goto(`http://127.0.0.1:${webPort}/subscription-aggregation`)
  await page.getByRole('button', { name: '预览节点', exact: true }).click()
  await page.getByRole('button', { name: /演示订阅/ }).click()
  const detail = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '演示订阅 · 节点列表' }) })
  await detail.getByText('香港 · 缓存节点', { exact: true }).waitFor()
  assert.match(await detail.innerText(), /共 2 个缓存节点/)
  await page.evaluate(() => document.fonts.ready)
  await mkdir(resolve(root, 'doc/img/guide'), { recursive: true })
  await detail.screenshot({ path: resolve(root, 'doc/img/guide/aggregation-subscription-cache.png'), animations: 'disabled' })
  await detail.getByRole('button', { name: '关闭', exact: true }).first().click()
  await detail.waitFor({ state: 'hidden' })
  await fetch(`${apiBase}/audit/update`, { method: 'POST' })
  await page.getByRole('button', { name: /演示订阅/ }).click()
  await detail.getByText('来源更新后的节点', { exact: true }).waitFor()
  assert.doesNotMatch(await detail.innerText(), /香港 · 缓存节点/)
  await detail.getByRole('button', { name: '关闭', exact: true }).first().click()
  await detail.waitFor({ state: 'hidden' })
  const preview = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '节点预览', exact: true }) })
  await preview.getByRole('button', { name: '关闭', exact: true }).click()
  await preview.waitFor({ state: 'hidden' })
  await page.getByRole('button', { name: '预览节点', exact: true }).click()
  await page.getByRole('button', { name: /来源更新后的节点/ }).waitFor()
  await fetch(`${apiBase}/audit/clear`, { method: 'POST' })
  await page.getByRole('button', { name: /演示订阅/ }).click()
  await detail.getByText('暂无可用缓存', { exact: true }).waitFor()
  await page.setViewportSize({ width: 375, height: 812 })
  await page.evaluate(() => { document.documentElement.classList.add('dark'); document.documentElement.dataset.motion = 'off' })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'mobile overflow')
  const state = await (await fetch(`${apiBase}/audit/state`)).json()
  assert.deepEqual(state, { attempts: [], history_unchanged: true })
  assert.deepEqual(downloads, [])
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ ...state, fetch_requests: downloads.length, source_update_visible: true, empty_cache: true, mobile: true }))
} catch (error) {
  console.error(logs.slice(-3000))
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
