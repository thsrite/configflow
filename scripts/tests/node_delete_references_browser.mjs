// Run only on the test server, after building frontend/. Uses synthetic data.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'

const root = process.cwd()
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const { chromium } = await import(process.env.CF_PLAYWRIGHT || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs')
const port = process.env.CF_NODE_DELETE_PORT || '15943'
const base = `http://127.0.0.1:${port}`
const scratch = await mkdtemp(join(tmpdir(), 'cf-node-delete-'))
const evidence = resolve(root, '.ci-results/node-deletion')
await mkdir(evidence, { recursive: true })
await mkdir(resolve(root, 'doc/img/guide'), { recursive: true })
const source = String.raw`
import os
from pathlib import Path
from flask import send_from_directory
from werkzeug.serving import run_simple
from backend.test_qa_integrity import make_app
from backend.test_dialer_proxy import seed_nodes

app, repo = make_app(Path(os.environ['DATA_DIR']))
def node(identity, name):
    return {'id': identity, 'name': name, 'type': 'http', 'enabled': True,
            'server': 'example.invalid', 'port': 8080,
            'proxy_string': '{"type":"http","server":"example.invalid","port":8080}'}
seed_nodes(repo, [node('one', '香港节点'), node('two', '日本节点')],
           aggregations=[{'id': 'agg', 'name': '常用聚合', 'nodes': ['one', 'two'], 'subscriptions': []}],
           groups=[{'id': 'group', 'name': '自动选择', 'type': 'select', 'manual_nodes': ['one', 'DIRECT'], 'aggregations': ['agg'],
                    'proxies_order': [{'type': 'node', 'id': 'one'}, {'type': 'node', 'id': 'DIRECT'}]}])
repo.create_profile({'id': 'other', 'name': '测试空间'})
repo.update_profile_transaction('other', lambda p: p['proxy_groups'].append(
    {'id': 'indirect', 'name': '代理出口', 'type': 'select', 'aggregations': ['agg']}))
frontend = Path.cwd() / 'frontend' / 'dist'
@app.route('/')
@app.route('/<path:path>')
def index(path=''):
    return send_from_directory(frontend, path if (frontend / path).is_file() else 'index.html')
run_simple('0.0.0.0', int(os.environ['CF_NODE_DELETE_PORT']), app, threaded=True)
`
const server = spawn(python, ['-c', source], {
  cwd: root, env: { ...process.env, DATA_DIR: scratch, CF_NODE_DELETE_PORT: port },
  stdio: ['ignore', 'pipe', 'pipe'],
})
let logs = ''
server.stdout.on('data', chunk => { logs = (logs + chunk).slice(-10000) })
server.stderr.on('data', chunk => { logs = (logs + chunk).slice(-10000) })
let browser
try {
  let ready = false
  for (let i = 0; i < 80; i++) {
    if (server.exitCode !== null) throw new Error(logs)
    try { if ((await fetch(`${base}/api/nodes`)).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  assert.ok(ready, 'Isolated preview did not start')
  browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, reducedMotion: 'reduce' })
  await context.addInitScript(() => {
    localStorage.setItem('configflow-nodes-view', 'list')
  })
  const page = await context.newPage()
  await page.goto(`${base}/nodes`)
  await page.getByRole('button', { name: '删除 香港节点', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '解除引用并删除', exact: true }).waitFor()
  assert.match(await dialog.innerText(), /常用聚合/)
  assert.match(await dialog.innerText(), /测试空间/)
  await page.screenshot({ path: resolve(root, 'doc/img/guide/nodes-delete-references.png'), animations: 'disabled' })
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.locator('html[data-theme="dark"]').waitFor()
  await page.screenshot({ path: join(evidence, 'dark.png'), animations: 'disabled' })

  await page.setViewportSize({ width: 375, height: 812 })
  await page.emulateMedia({ colorScheme: 'light' })
  await page.locator('html[data-theme="light"]').waitFor()
  await page.screenshot({ path: join(evidence, 'mobile.png'), animations: 'disabled' })
  const overflow = await dialog.evaluate(element => element.scrollWidth > element.clientWidth)
  assert.equal(overflow, false, 'Mobile dialog must not overflow horizontally')
  const footer = await dialog.getByRole('button', { name: '解除引用并删除', exact: true }).boundingBox()
  assert.ok(footer && footer.y >= 0 && footer.y + footer.height <= 812, 'Confirmation remains visible on mobile')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal((await (await page.request.get(`${base}/api/nodes`)).json()).length, 2)

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.getByRole('button', { name: '删除 香港节点', exact: true }).click()
  await dialog.getByRole('button', { name: '解除引用并删除', exact: true }).waitFor()
  // A new reference after preview requires a new explicit confirmation.
  const added = await page.request.post(`${base}/api/profiles/other/proxy-groups`, { data: {
    id: 'concurrent', name: '后来添加的引用', type: 'select', manual_nodes: ['one', 'DIRECT'],
  } })
  assert.ok(added.ok())
  await dialog.getByRole('button', { name: '解除引用并删除', exact: true }).click()
  await dialog.getByText('节点或引用关系已变化，请重新查看删除影响并确认').waitFor()
  assert.equal((await (await page.request.get(`${base}/api/nodes`)).json()).length, 2)
  await dialog.getByRole('button', { name: '重新读取影响', exact: true }).click()
  await dialog.getByText('测试空间 → 策略组「后来添加的引用」', { exact: true }).waitFor()
  await dialog.getByRole('button', { name: '解除引用并删除', exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
  assert.deepEqual((await (await page.request.get(`${base}/api/nodes`)).json()).map(node => node.id), ['two'])
  const groups = await (await page.request.get(`${base}/api/profiles/other/proxy-groups`)).json()
  assert.deepEqual(groups.find(group => group.id === 'concurrent').manual_nodes, ['DIRECT'])

  // Batch entry uses the same transaction, including an emptied aggregation.
  await page.getByRole('button', { name: '全选', exact: true }).click()
  await page.getByRole('button', { name: '删除 1 项', exact: true }).click()
  await dialog.getByText('以下聚合或策略组将没有已选择的来源，请确认符合预期：').waitFor()
  await dialog.getByRole('button', { name: '解除引用并删除', exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
  assert.deepEqual(await (await page.request.get(`${base}/api/nodes`)).json(), [])
  console.log('PASS: deletion preview, mobile layout, cancel, stale confirmation, cross-profile cleanup and batch entry')
} catch (error) {
  console.error(logs)
  throw error
} finally {
  await browser?.close()
  server.kill('SIGTERM')
  await new Promise(resolve => server.exitCode !== null ? resolve() : server.once('exit', resolve))
  await rm(scratch, { recursive: true, force: true })
}
