// Run on the test server after building frontend/. All data is synthetic.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'

const root = process.cwd()
const { chromium } = await import(process.env.CF_PLAYWRIGHT || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs')
const port = process.env.CF_NODE_SELECTION_PORT || '15944'
const base = `http://127.0.0.1:${port}`
const scratch = await mkdtemp(join(tmpdir(), 'cf-node-selection-'))
const evidence = resolve(root, '.ci-results/node-selection')
await mkdir(evidence, { recursive: true })
const source = String.raw`
import os
from pathlib import Path
from flask import send_from_directory
from werkzeug.serving import run_simple
from backend.test_qa_integrity import make_app
from backend.test_dialer_proxy import seed_nodes

app, repo = make_app(Path(os.environ['DATA_DIR']))
names = ['香港 · 日常', '日本 · 备用', '新加坡 · 工作', '美国 · 测试',
         '德国 · 停用节点', '香港 · 长名称节点用于检查内容截断与多选操作']
seed_nodes(repo, [{'id': str(i), 'name': name, 'type': 'http', 'enabled': i != 4,
                  'proxy_string': '{"type":"http","server":"example.invalid","port":8080}'}
                 for i, name in enumerate(names)])
frontend = Path.cwd() / 'frontend' / 'dist'
@app.route('/')
@app.route('/<path:path>')
def index(path=''):
    return send_from_directory(frontend, path if (frontend / path).is_file() else 'index.html')
run_simple('127.0.0.1', int(os.environ['CF_NODE_SELECTION_PORT']), app, threaded=True)
`
const server = spawn(process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python', ['-c', source], {
  cwd: root, env: { ...process.env, DATA_DIR: scratch, CF_NODE_SELECTION_PORT: port },
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
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, reducedMotion: 'reduce', colorScheme: 'light' })
  await context.addInitScript(() => localStorage.setItem('configflow-nodes-view', 'card'))
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto(`${base}/nodes`)
  const first = page.locator('[data-name="香港 · 日常"]')
  const second = page.locator('[data-name="日本 · 备用"]')
  const checkbox = first.getByRole('checkbox')
  await first.getByText('未测速', { exact: true }).click()
  assert.equal(await checkbox.isChecked(), true, 'Card content selects the node')
  await second.getByText('未测速', { exact: true }).click()
  await page.getByRole('button', { name: '删除 2 项', exact: true }).waitFor()
  await page.screenshot({ path: resolve(root, 'doc/img/guide/nodes-card-selection.png'), animations: 'disabled' })

  // Existing controls keep their actions and selection does not toggle twice.
  await checkbox.click()
  assert.equal(await checkbox.isChecked(), false)
  await checkbox.focus()
  await page.keyboard.press('Space')
  assert.equal(await checkbox.isChecked(), true, 'Keyboard selection remains available')
  await first.getByRole('button', { name: '编辑 香港 · 日常', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(await checkbox.isChecked(), true, 'Edit does not toggle selection')
  await first.getByRole('button', { name: '香港 · 日常 的更多操作', exact: true }).click()
  await page.getByRole('menuitem', { name: '节点字符串', exact: true }).click()
  await page.getByRole('dialog').locator('button[data-slot="button"]').filter({ hasText: /^关闭$/ }).click()
  assert.equal(await checkbox.isChecked(), true, 'Menu actions do not toggle selection')
  await first.getByRole('button', { name: '停用 香港 · 日常', exact: true }).click()
  await first.getByRole('button', { name: '启用 香港 · 日常', exact: true }).waitFor()
  assert.equal(await checkbox.isChecked(), true, 'Enable switch does not toggle selection')

  // Real drag-selection of text should not change the node selection.
  const title = first.locator('header span[title]')
  const box = await title.boundingBox()
  assert.ok(box)
  await page.mouse.move(box.x + 1, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width - 1, box.y + box.height / 2, { steps: 8 })
  await page.mouse.up()
  assert.ok(await page.evaluate(() => window.getSelection()?.toString().length > 0))
  assert.equal(await checkbox.isChecked(), true, 'Copying text does not toggle selection')
  await page.evaluate(() => window.getSelection()?.removeAllRanges())

  await page.getByRole('button', { name: '调整顺序', exact: true }).click()
  await first.click({ position: { x: 8, y: 8 } })
  await page.getByRole('region', { name: '排序模式' }).getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(await checkbox.isChecked(), true, 'Sorting does not toggle selection')
  await page.getByRole('button', { name: '列表视图', exact: true }).click()
  assert.equal(await checkbox.isChecked(), true, 'Changing views retains selection')
  await first.getByText('香港 · 日常', { exact: true }).click()
  assert.equal(await checkbox.isChecked(), false, 'List content toggles selection')
  assert.equal(await second.getAttribute('data-state'), 'selected')
  await page.screenshot({ path: join(evidence, 'list.png'), animations: 'disabled' })

  await page.getByRole('button', { name: '卡片视图', exact: true }).click()
  await first.getByText('未测速', { exact: true }).click()
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.locator('html[data-theme="dark"]').waitFor()
  await page.screenshot({ path: join(evidence, 'dark.png'), animations: 'disabled' })
  assert.deepEqual(errors, [], 'No browser errors')

  const mobile = await browser.newContext({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true, reducedMotion: 'reduce', colorScheme: 'light' })
  const phone = await mobile.newPage()
  await phone.goto(`${base}/nodes`)
  const phoneCard = phone.locator('[data-name="香港 · 日常"]')
  await phoneCard.getByText('未测速', { exact: true }).tap()
  assert.equal(await phoneCard.getByRole('checkbox').isChecked(), true, 'Touch card content selects')
  await phone.screenshot({ path: join(evidence, 'mobile.png'), animations: 'disabled' })
  assert.equal(await phone.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'No mobile horizontal overflow')
  await phoneCard.getByText('未测速', { exact: true }).tap()
  assert.equal(await phoneCard.getByRole('checkbox').isChecked(), false, 'Second tap clears selection')
  console.log('PASS: card/list content, checkbox, keyboard, edit/menu/enable isolation, text selection, sorting, themes and mobile touch')
} catch (error) {
  console.error(logs.split('\n').filter(line => !line.includes('/assets/')).slice(-20).join('\n'))
  throw error
} finally {
  await browser?.close()
  server.kill('SIGTERM')
  await new Promise(resolve => server.exitCode !== null ? resolve() : server.once('exit', resolve))
  await rm(scratch, { recursive: true, force: true })
}
