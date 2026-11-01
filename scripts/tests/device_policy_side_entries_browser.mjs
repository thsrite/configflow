// Run only on the test server, after building frontend/. Uses synthetic data in a temporary database.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'

const root = process.cwd()
const python = process.env.CF_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const { chromium } = await import(process.env.CF_PLAYWRIGHT || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs')
const port = process.env.CF_SIDE_ENTRIES_PORT || '15947'
const base = `http://127.0.0.1:${port}`
const scratch = await mkdtemp(join(tmpdir(), 'cf-side-entries-'))
const evidence = resolve(root, '.ci-results/device-policy-side-entries')
await mkdir(evidence, { recursive: true })
await mkdir(resolve(root, 'doc/img/guide'), { recursive: true })
const source = String.raw`
import os
from copy import deepcopy
from pathlib import Path
import yaml
from flask import send_from_directory
from werkzeug.serving import run_simple
from backend.common.agent_manager import get_agent_manager
from backend.common.oxidns_device_policy import apply_policies
from backend.common.oxidns_editor import prepare_editor
from backend.test_qa_integrity import make_app

app, repo = make_app(Path(os.environ['DATA_DIR']))
def chain(prefix, listen, upstream, recorder=False):
    steps = ([{'exec': '$query_recorder'}] if recorder else []) + [
        {'exec': '$' + prefix + '_forward'}, {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]
    return [{'tag': prefix + '_forward', 'type': 'forward', 'args': {'upstreams': [{'addr': upstream}]}},
            {'tag': prefix + '_sequence', 'type': 'sequence', 'args': steps},
            *[{'tag': prefix + '_' + p, 'type': p + '_server', 'args': {'listen': listen, 'entry': prefix + '_sequence'}}
              for p in ('udp', 'tcp')]]
get_agent_manager().register_agent({'name': '家里 OxiDNS', 'host': '198.51.100.2', 'port': 18081,
                                    'service_type': 'oxidns', 'profile_id': 'default'})
repo.update_system_transaction(lambda system: system['system_config'].update(
    node_test_dns=['198.51.100.2:15353'], node_test_dns_config={
        'mode': 'custom', 'servers': ['198.51.100.2:15353'], 'bootstrap': [], 'managed': {}}))
def seed(profile):
    config = {'oxidns': {'query_recorder': {'enabled': True}}}
    settings = prepare_editor(config['oxidns'], config)
    document = yaml.safe_load(settings['native_config'])
    document['plugins'] += chain('node_test_real', '198.51.100.2:15353', 'udp://198.51.100.53', recorder=True)
    document['plugins'] += chain('debug_dns', '198.51.100.2:5300', 'udp://198.51.100.54')
    settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    profile['oxidns'] = apply_policies(settings, profile, [
        {'ip': '198.51.100.21', 'name': '客厅电视', 'mode': 'direct'},
        {'ip': '198.51.100.22', 'name': '书房电脑', 'mode': 'smart'}])
    profile['device_notes'] = {'198.51.100.21': '客厅电视', '198.51.100.22': '书房电脑'}
repo.update_profile_transaction('default', seed)
frontend = Path.cwd() / 'frontend' / 'dist'
@app.route('/')
@app.route('/<path:path>')
def index(path=''):
    return send_from_directory(frontend, path if (frontend / path).is_file() else 'index.html')
run_simple('0.0.0.0', int(os.environ['CF_SIDE_ENTRIES_PORT']), app, threaded=True)
`
const server = spawn(python, ['-c', source], {
  cwd: root, env: { ...process.env, DATA_DIR: scratch, CF_SIDE_ENTRIES_PORT: port },
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
    try { if ((await fetch(`${base}/api/client-devices?profile=default`)).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  assert.ok(ready, 'Isolated preview did not start')
  const state = await (await fetch(`${base}/api/client-devices?profile=default`)).json()
  assert.equal(state.editable, true, state.reason)
  assert.deepEqual(state.side_entries, [{ tags: ['debug_dns_udp', 'debug_dns_tcp'], listen: '198.51.100.2:5300', entry: 'debug_dns_sequence' }])

  browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 }, reducedMotion: 'reduce' })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(String(error)))
  await page.goto(`${base}/client-devices`)
  const note = page.locator('[data-side-entries]')
  await note.waitFor()
  const text = await note.innerText()
  assert.match(text, /以下入口没有经过主处理链，设备策略对它们不生效/)
  assert.match(text, /debug_dns_udp、debug_dns_tcp（198\.51\.100\.2:5300）/)
  assert.doesNotMatch(text, /node_test_real/)
  assert.equal(await page.locator('[aria-label="198.51.100.21 DNS 策略"]').first().isDisabled(), false)
  await page.screenshot({ path: resolve(root, 'doc/img/guide/client-devices-side-entries.png'), animations: 'disabled' })

  await page.emulateMedia({ colorScheme: 'dark' })
  await page.setViewportSize({ width: 375, height: 812 })
  await page.reload()
  await note.waitFor()
  await page.screenshot({ path: join(evidence, 'mobile-dark.png'), animations: 'disabled' })
  const overflow = await page.evaluate(() => document.scrollingElement.scrollWidth > window.innerWidth)
  assert.equal(overflow, false, 'Mobile page must not overflow horizontally')
  assert.deepEqual(errors, [])
  console.log('PASS: side entry note, node-test entry hidden, policies editable, mobile without overflow')
} catch (error) {
  console.error(logs)
  throw error
} finally {
  await browser?.close()
  server.kill('SIGTERM')
  await new Promise(resolve => server.exitCode !== null ? resolve() : server.once('exit', resolve))
  await rm(scratch, { recursive: true, force: true })
}
